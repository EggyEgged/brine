"""Tiny MCP stdio client used by the live tests (scripts/*-test.py)."""
import json, os, pathlib, queue, shutil, subprocess, sys, threading, time

BRINE = os.environ.get("BRINE") or shutil.which("brine")


def step(msg):
    print(f"--> {msg}", flush=True)


def fail(msg):
    print(f"FAIL: {msg}", file=sys.stderr, flush=True)
    subprocess.run([BRINE, "kill"], capture_output=True)
    sys.exit(1)


def prefix_dir():
    data = os.environ.get("XDG_DATA_HOME") or os.path.expanduser("~/.local/share")
    return os.path.join(data, "brine", "prefix")


def _procs(exe, in_prefix=True):
    """PIDs of this user running the Windows program `exe` (argv[0], or argv[1]
    after a wine loader). Matching the real argv, not a substring of the whole
    command line, keeps WebView2 children and shells that merely mention the
    name from counting."""
    res = []
    exe = exe.lower().encode()
    for d in os.listdir("/proc"):
        if not d.isdigit():
            continue
        try:
            if os.stat(f"/proc/{d}").st_uid != os.getuid():
                continue
            cmd = pathlib.Path(f"/proc/{d}/cmdline").read_bytes().split(b"\0")
        except OSError:
            continue
        base = lambda c: c.replace(b"\\", b"/").rsplit(b"/", 1)[-1].lower()
        if cmd and (base(cmd[0]) == exe or (base(cmd[0]).startswith(b"wine") and len(cmd) > 1 and base(cmd[1]) == exe)):
            if in_prefix:  # ignore processes of other prefixes (other tools, parallel tests)
                try:
                    env = pathlib.Path(f"/proc/{d}/environ").read_bytes().split(b"\0")
                except OSError:
                    continue
                if f"WINEPREFIX={prefix_dir()}".encode() not in env:
                    continue
            res.append(int(d))
    return res


def studio_pids():
    return _procs("RobloxStudioBeta.exe")


def helper_pids():
    return _procs("StudioMCP.exe")


def wineserver_pids():
    return _procs("wineserver") + _procs("wineserver64")


def start_studio(place):
    subprocess.Popen([BRINE, "run", str(place)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
    end = time.time() + 60
    while time.time() < end and not studio_pids():
        time.sleep(1)
    if not studio_pids():
        fail("Studio did not start")


class Session:
    """One `brine mcp` process, spoken to the way an MCP client would."""

    def __init__(self, name="test"):
        self.name = name
        self.p = subprocess.Popen([BRINE, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        self.q = queue.Queue()
        self.notes = []
        self.n = 0
        self.lock = threading.Lock()
        threading.Thread(target=self._read, daemon=True).start()
        r = self.call("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": name, "version": "0"}})
        if "result" not in r:
            fail(f"{name}: initialize failed: {r}")
        self.send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def _read(self):
        for line in self.p.stdout:
            self.q.put(line)

    def send(self, o):
        self.p.stdin.write((json.dumps(o) + "\n").encode())
        self.p.stdin.flush()

    def call(self, method, params, timeout=60):
        with self.lock:  # one outstanding request per session keeps the reader simple
            self.n += 1
            i = self.n
            self.send({"jsonrpc": "2.0", "id": i, "method": method, "params": params})
            end = time.time() + timeout
            while time.time() < end:
                try:
                    line = self.q.get(timeout=1)
                except queue.Empty:
                    continue
                m = json.loads(line)  # raises if stdout is not clean JSON
                if m.get("id") == i:
                    return m
                if "method" in m:
                    self.notes.append(m["method"])
        fail(f"{self.name}: {method}: no response in {timeout}s")

    def tool(self, name, args, timeout=60):
        m = self.call("tools/call", {"name": name, "arguments": args}, timeout)
        r = m.get("result") or {}
        text = "\n".join(c.get("text", "") for c in r.get("content", []))
        return text, bool(r.get("isError"))

    def studios(self):
        text, err = self.tool("list_roblox_studios", {})
        return [] if err else json.loads(text).get("studios", [])

    def luau(self, sid, code):
        return self.tool("execute_luau", {"studio_id": sid, "datamodel_type": "Edit", "code": code}, 90)

    def wait_studio(self, timeout=150):
        end = time.time() + timeout
        while time.time() < end:
            st = self.studios()
            if st and st[0].get("name"):
                return st[0]["id"]
            time.sleep(3)
        fail(f"{self.name}: Studio never appeared in list_roblox_studios")

    def close(self, timeout=15):
        t0 = time.time()
        self.p.stdin.close()
        try:
            self.p.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            fail(f"{self.name}: brine mcp did not exit after stdin closed")
        return time.time() - t0
