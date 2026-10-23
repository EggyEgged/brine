#!/usr/bin/env python3
"""Live test: an MCP session survives `brine kill` and a Studio restart.

Needs a signed-in Studio with "Enable Studio as MCP server" on. Starts Studio
with a temporary place, opens ONE `brine mcp` session, then kills Studio and
its wineserver, relaunches Studio and checks that the same session works again.

    BRINE=/path/to/brine scripts/restart-test.py
"""
import json, os, pathlib, shutil, subprocess, sys, tempfile, threading, time, queue

BRINE = os.environ.get("BRINE") or shutil.which("brine")
HERE = pathlib.Path(__file__).resolve().parent
work = pathlib.Path(tempfile.mkdtemp(prefix="brine-restart-"))
place = work / "Restart.rbxlx"
shutil.copy(HERE / "fixtures" / "Integration.rbxlx", place)


def step(msg):
    print(f"--> {msg}", flush=True)


def fail(msg):
    print(f"FAIL: {msg}", file=sys.stderr, flush=True)
    subprocess.run([BRINE, "kill"], capture_output=True)
    sys.exit(1)


def studio_pids():
    out = subprocess.run(["pgrep", "-f", r"RobloxStudioBeta\.exe"], capture_output=True, text=True).stdout.split()
    res = []
    for p in out:
        try:
            cmd = pathlib.Path(f"/proc/{p}/cmdline").read_bytes().split(b"\0")
        except OSError:
            continue
        if cmd and b"msedgewebview2" not in cmd[0] and any(c.endswith(b"RobloxStudioBeta.exe") for c in cmd[:2]):
            res.append(int(p))
    return res


def wine_leftovers():
    out = subprocess.run(["pgrep", "-u", str(os.getuid()), "-f", r"StudioMCP\.exe|wineserver"], capture_output=True, text=True).stdout.split()
    return out


class Session:
    def __init__(self):
        self.p = subprocess.Popen([BRINE, "mcp"], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        self.q = queue.Queue()
        self.notes = []
        self.n = 0
        threading.Thread(target=self._read, daemon=True).start()

    def _read(self):
        for line in self.p.stdout:
            self.q.put(line)

    def send(self, o):
        self.p.stdin.write((json.dumps(o) + "\n").encode())
        self.p.stdin.flush()

    def call(self, method, params, timeout=60):
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
        fail(f"{method}: no response in {timeout}s")

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


def start_studio():
    subprocess.Popen([BRINE, "run", str(place)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
    end = time.time() + 60
    while time.time() < end and not studio_pids():
        time.sleep(1)
    if not studio_pids():
        fail("Studio did not start")


def wait_studio(s, timeout=150):
    end = time.time() + timeout
    while time.time() < end:
        st = s.studios()
        if st and st[0].get("name"):
            return st[0]["id"]
        time.sleep(3)
    fail("Studio never appeared in list_roblox_studios")


try:
    step("fresh state: stopping any Studio")
    subprocess.run([BRINE, "kill"], check=False)
    time.sleep(2)

    s = Session()
    r = s.call("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": "restart-test", "version": "0"}})
    assert "result" in r, r
    s.send({"jsonrpc": "2.0", "method": "notifications/initialized"})

    step("Studio closed: tool call must fail fast with a clear error")
    t0 = time.time()
    text, err = s.tool("list_roblox_studios", {})
    t_closed = time.time() - t0
    # list_roblox_studios is answered by the helper itself (empty list); execute_luau is intercepted.
    text2, err2 = s.tool("execute_luau", {"studio_id": "x", "datamodel_type": "Edit", "code": "return 1"})
    if not err2 or "not running" not in text2 or time.time() - t0 > 5:
        fail(f"expected fast 'not running' error, got err={err2} text={text2!r}")
    print(f"    ok ({time.time() - t0:.2f}s): {text2[:70]}...")

    step("start Studio, wait for it to connect")
    start_studio()
    sid = wait_studio(s)
    out, err = s.luau(sid, 'print("before-kill") return "A"')
    if err or "A" not in out:
        fail(f"execute_luau before kill failed: {out!r}")
    print(f"    ok: studio {sid}, luau -> {out!r}")

    step("brine kill while the session is connected")
    k = subprocess.run([BRINE, "kill"], capture_output=True, text=True)
    print("    " + k.stderr.strip())
    time.sleep(1)
    if studio_pids():
        fail("Studio still running after brine kill")

    step("call during the outage must not hang")
    t0 = time.time()
    out, err = s.luau(sid, "return 2")
    if not err or time.time() - t0 > 20:
        fail(f"expected a quick error during outage, got err={err} out={out!r} after {time.time() - t0:.1f}s")
    print(f"    ok ({time.time() - t0:.1f}s): {out[:80]}")

    step("relaunch Studio; the SAME session must work again")
    start_studio()
    sid2 = wait_studio(s)
    out, err = s.luau(sid2, 'print("after-restart") return "B"')
    if err or "B" not in out:
        fail(f"execute_luau after restart failed: {out!r}")
    print(f"    ok: studio {sid2}, luau -> {out!r}; notifications seen: {sorted(set(s.notes))}")

    step("close stdin: brine mcp must exit and leave no helper behind")
    t0 = time.time()
    s.p.stdin.close()
    try:
        s.p.wait(timeout=15)
    except subprocess.TimeoutExpired:
        fail("brine mcp did not exit after stdin closed")
    print(f"    exited in {time.time() - t0:.2f}s")
    time.sleep(2)
    leftover = subprocess.run(["pgrep", "-a", "-u", str(os.getuid()), "-f", r"StudioMCP\.exe"], capture_output=True, text=True).stdout
    if leftover.strip():
        fail("StudioMCP.exe left behind:\n" + leftover)

    subprocess.run([BRINE, "kill"], check=False)
    time.sleep(2)
    left = wine_leftovers()
    if left:
        fail(f"wine processes left after final kill: {left}")
    print("PASS")
finally:
    shutil.rmtree(work, ignore_errors=True)
