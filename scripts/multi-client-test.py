#!/usr/bin/env python3
"""Live test: several MCP clients (say Claude Code and Claude Desktop) at once.

Each client runs its own `brine mcp`, and so its own StudioMCP.exe. The first
one hosts the port Studio connects to; the others join it. This checks that
concurrent calls don't cross wires, that the clients keep working when the
one hosting the port exits, and that a late client can still join.

    BRINE=/path/to/brine scripts/multi-client-test.py
"""
import os, pathlib, shutil, subprocess, sys, tempfile, threading, time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from mcpsession import BRINE, Session, fail, helper_pids, preflight, start_studio, step, wineserver_pids

HERE = pathlib.Path(__file__).resolve().parent
work = pathlib.Path(tempfile.mkdtemp(prefix="brine-multi-"))
place = work / "Multi.rbxlx"
shutil.copy(HERE / "fixtures" / "Integration.rbxlx", place)


def hammer(s, sid, tag, n, errors):
    for i in range(n):
        marker = f"{tag}-{i}"
        out, err = s.luau(sid, f'return "{marker}"')
        if err or marker not in out:
            errors.append(f"{tag}: call {i} returned {out!r} (error={err})")
            return


def retry_luau(s, sid, marker, timeout=45):
    end = time.time() + timeout
    last = None
    while time.time() < end:
        out, err = s.luau(sid, f'return "{marker}"')
        if not err and marker in out:
            return time.time()
        last = out
        time.sleep(2)
    fail(f"{s.name}: never recovered, last reply {last!r}")


preflight()
try:
    step("fresh state")
    subprocess.run([BRINE, "kill"], check=False, capture_output=True)
    time.sleep(2)

    step("client A starts first (it will host the helper port), then Studio")
    a = Session("A")
    start_studio(place)
    sid = a.wait_studio()
    out, err = a.luau(sid, 'return "A-ready"')
    assert not err and "A-ready" in out, out

    step("client B joins while A is connected")
    b = Session("B")
    sb = b.wait_studio()
    if sb != sid:
        fail(f"B sees a different Studio ({sb} vs {sid})")
    out, err = b.luau(sid, 'return "B-ready"')
    if err or "B-ready" not in out:
        fail(f"B cannot run code: {out!r}")
    print("    ok: both clients see Studio", sid)

    step("concurrent traffic: 12 calls each, replies must not cross")
    errors = []
    ts = [threading.Thread(target=hammer, args=(s, sid, tag, 12, errors)) for s, tag in ((a, "alpha"), (b, "beta"))]
    t0 = time.time()
    [t.start() for t in ts]
    [t.join() for t in ts]
    if errors:
        fail("; ".join(errors))
    print(f"    ok: 24 calls in {time.time() - t0:.1f}s")

    step("A exits (it hosted the port); B must keep working")
    print(f"    A exited in {a.close():.2f}s")
    t0 = time.time()
    retry_luau(b, sid, "B-after-A")
    print(f"    ok: B recovered after {time.time() - t0:.1f}s")

    step("client C joins late")
    c = Session("C")
    sc = c.wait_studio()
    out, err = c.luau(sc, 'return "C-ready"')
    if err or "C-ready" not in out:
        fail(f"C cannot run code: {out!r}")
    out, err = b.luau(sc, 'return "B-still"')
    if err or "B-still" not in out:
        fail(f"B broke when C joined: {out!r}")
    print("    ok")

    step("close everything: no helper may be left behind")
    print(f"    B exited in {b.close():.2f}s, C exited in {c.close():.2f}s")
    time.sleep(2)
    if helper_pids():
        fail(f"StudioMCP.exe left behind: {helper_pids()}")
    subprocess.run([BRINE, "kill"], check=False, capture_output=True)
    time.sleep(2)
    if wineserver_pids():
        fail(f"wineserver left behind: {wineserver_pids()}")
    print("PASS")
finally:
    shutil.rmtree(work, ignore_errors=True)
