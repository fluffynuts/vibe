import os, sys, uuid; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
a = Agent("e5")
a.prompt("Reply with just: one.")
a.wait_results(1, 40)
a.send({"type": "control_request", "request_id": "m-1", "request": {"subtype": "set_model", "model": "claude-sonnet-5-5"}}, "control_request set_model claude-sonnet-5-5")
time.sleep(2)
a.prompt("Reply with just: two.")
a.wait_results(2, 40)
a.send({"type": "control_request", "request_id": "m-2", "request": {"subtype": "set_model", "model": "claude-haiku-5-5"}}, "control_request set_model back to haiku")
time.sleep(1)
a.prompt("Reply with just: three.")
a.wait_results(3, 40)
models = [(e["_t"], e.get("model")) for e in a.events if e.get("type") == "system" and e.get("subtype") == "init"]
print("-- model on each init:", models)
amodels = [(e["_t"], e.get("message", {}).get("model")) for e in a.events if e.get("type") == "assistant"]
print("-- model on assistant messages:", amodels)
a.close()
