import os, sys, uuid; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
# E13: can a page stop the agent's background tasks without ending the agent? `stop_task` per task (from background_tasks_changed).
a = Agent("e13")
a.prompt("Use the Bash tool with run_in_background=true to run exactly: sleep 41; echo E13DONE. Do NOT wait for it or poll it. Reply with just the word: started.", uuid=str(uuid.uuid4()))
a.wait_results(1, 60)
tasks = [e for e in a.events if e.get("subtype") == "background_tasks_changed"][-1].get("tasks", [])
print(f"-- background tasks listed: {[(t['task_id'], t['task_type'], t['description']) for t in tasks]}")
for t in tasks:
    a.send({"type": "control_request", "request_id": "st-" + t["task_id"], "request": {"subtype": "stop_task", "task_id": t["task_id"]}}, "control_request stop_task " + t["task_id"])
time.sleep(4)
left = [e for e in a.events if e.get("subtype") == "background_tasks_changed"][-1].get("tasks", [])
print(f"-- background tasks now: {left}; agent alive={a.alive()}")
a.prompt("Reply with just: still-here.", uuid=str(uuid.uuid4()))
a.wait_results(2, 30)
a.close()
