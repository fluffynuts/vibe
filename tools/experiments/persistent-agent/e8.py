import os, sys, json; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
a = Agent("e8", extra=("--effort", "low"))
a.prompt("Reply with just: one.")
a.wait_results(1, 40)
init = [e for e in a.events if e.get("subtype") == "init"][0]
print("-- init keys mentioning effort:", {k: v for k, v in init.items() if "ffort" in k or "ffort" in json.dumps(v)[:0]})
for i, req in enumerate([
    {"subtype": "apply_flag_settings", "settings": {"effortLevel": "high"}},
    {"subtype": "update_settings", "settings": {"effortLevel": "medium"}},
]):
    a.send({"type": "control_request", "request_id": f"f-{i}", "request": req}, "control_request " + json.dumps(req))
    time.sleep(1.5)
a.prompt("Reply with just: two.")
a.wait_results(2, 40)
init2 = [e for e in a.events if e.get("subtype") == "init"][-1]
print("-- second init effort-ish:", {k: v for k, v in init2.items() if "ffort" in k})
a.close()
