import os, sys, subprocess; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
def pg(pattern):
    r = subprocess.run(["pgrep", "-f", pattern], capture_output=True, text=True)
    return [p for p in r.stdout.split() if p]
a = Agent("e6")
a.prompt("Use the Bash tool with run_in_background=true to run exactly: sleep 47; echo E6DONE. Do NOT wait for it or poll it. Reply with just the word: started.")
a.wait_results(1, 40)
sid = [e for e in a.events if e.get("subtype") == "init"][0]["session_id"]
print(f"-- session {sid}; sleep pids before closing stdin: {pg('sleep 47')}; claude pid {a.p.pid}")
t = time.time()
a.p.stdin.close()
while a.alive() and time.time() - t < 20:
    time.sleep(0.2)
print(f"-- after stdin closed: claude alive={a.alive()} (took {time.time()-t:.1f}s); sleep pids now: {pg('sleep 47')}")
time.sleep(3)
print(f"-- 3s later: sleep pids: {pg('sleep 47')}")
b = Agent("e6-resume", extra=("--resume", sid))
b.prompt("In one sentence: was there a background task earlier in this conversation, and do you know what became of it?")
b.wait_results(1, 40)
print(f"-- resumed run: {len(b.results())} result(s); session id same? {[e for e in b.events if e.get('subtype')=='init'][0]['session_id'] == sid}")
b.close()
print(f"-- sleep pids at the end: {pg('sleep 47')}")
