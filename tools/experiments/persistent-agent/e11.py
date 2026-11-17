import os, sys, uuid; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
# E11 (sbx only): vibe dies while the agent is running: the sbx client is killed. Does the agent in the sandbox end, or carry on unseen?
# wrapTurn's own comment says it carried on. A persistent agent that outlives vibe would need finding again. Ends with vibe's hard stop.
assert SBX, "set EXP_SBX to the sandbox's name"
a = Agent("e11")
a.prompt("Use the Bash tool with run_in_background=true to run exactly: sleep 47; echo E11DONE. Do NOT wait for it or poll it. Reply with just the word: started.", uuid=str(uuid.uuid4()))
a.wait_results(1, 60)
pid = a.agent_pid_in_sandbox()
print(f"-- agent's wrapper pid in the sandbox: {pid}")
a.p.kill()
print("-- killed the sbx client (as if vibe had died)")
time.sleep(5)
print(f"-- 5 s later the wrapper is alive in the sandbox: {a.agent_pid_in_sandbox()}")
print(f"-- hard stop says: {a.hard_stop()!r}")
time.sleep(1)
print(f"-- after the hard stop the wrapper is alive: {a.agent_pid_in_sandbox()}")
print("-- leftover `sleep 47` in the sandbox:", repr(a.in_sandbox("pgrep -f '[s]leep 47' | tr '\\n' ' '")))
