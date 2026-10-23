#!/usr/bin/env python3
"""Live test: an MCP session survives `brine kill` and a Studio restart.

Needs a signed-in Studio with "Enable Studio as MCP server" on. Starts Studio
with a temporary place, opens ONE `brine mcp` session, then kills Studio and
its wineserver, relaunches Studio and checks that the same session works again.

    BRINE=/path/to/brine scripts/restart-test.py
"""
import os, pathlib, shutil, subprocess, sys, tempfile, time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from mcpsession import BRINE, Session, fail, helper_pids, preflight, start_studio, step, studio_pids, wineserver_pids

HERE = pathlib.Path(__file__).resolve().parent
work = pathlib.Path(tempfile.mkdtemp(prefix="brine-restart-"))
place = work / "Restart.rbxlx"
shutil.copy(HERE / "fixtures" / "Integration.rbxlx", place)


preflight()
try:
    step("fresh state: stopping any Studio")
    subprocess.run([BRINE, "kill"], check=False)
    time.sleep(2)

    s = Session("restart-test")

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
    start_studio(place)
    sid = s.wait_studio()
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
    start_studio(place)
    sid2 = s.wait_studio()
    out, err = s.luau(sid2, 'print("after-restart") return "B"')
    if err or "B" not in out:
        fail(f"execute_luau after restart failed: {out!r}")
    print(f"    ok: studio {sid2}, luau -> {out!r}; notifications seen: {sorted(set(s.notes))}")

    step("close stdin: brine mcp must exit and leave no helper behind")
    print(f"    exited in {s.close():.2f}s")
    time.sleep(2)
    if helper_pids():
        fail(f"StudioMCP.exe left behind: {helper_pids()}")

    subprocess.run([BRINE, "kill"], check=False)
    time.sleep(2)
    if wineserver_pids() or helper_pids():
        fail(f"wine processes left after final kill: {wineserver_pids() + helper_pids()}")
    print("PASS")
finally:
    shutil.rmtree(work, ignore_errors=True)
