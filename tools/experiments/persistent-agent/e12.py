import os, sys, uuid; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
# E12 (sbx only): vibe's hard stop (stopTurnScript) on a persistent agent with a background task: does it end the whole tree, the
# background command too, within its 5 s, and does the stream close?
assert SBX, "set EXP_SBX to the sandbox's name"
a = Agent("e12")
a.prompt("Use the Bash tool with run_in_background=true to run exactly: sleep 53; echo E12DONE. Do NOT wait for it or poll it. Reply with just the word: started.", uuid=str(uuid.uuid4()))
a.wait_results(1, 60)
print(f"-- before: wrapper {a.agent_pid_in_sandbox()}; `sleep 53` pids: {a.in_sandbox(chr(112)+'grep -f \"[s]leep 53\" | tr \"\\n\" \" \"')!r}")
t = time.time()
print(f"-- hard stop says: {a.hard_stop()!r} (took {time.time()-t:.1f}s)")
time.sleep(1)
print(f"-- after: wrapper {a.agent_pid_in_sandbox()}; `sleep 53` pids: {a.in_sandbox(chr(112)+'grep -f \"[s]leep 53\" | tr \"\\n\" \" \"')!r}; client alive={a.alive()}")
a.close()
