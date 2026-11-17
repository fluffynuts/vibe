import os, sys, uuid; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
a = Agent("e3later")
ua, ub = str(uuid.uuid4()), str(uuid.uuid4())
a.prompt("Use the Bash tool in the foreground to run exactly: python3 -c \"import time; time.sleep(10); print('A-DONE')\"  Then reply with just: first-done.", uuid=ua)
time.sleep(4)
a.prompt("Reply with just: second-done.", uuid=ub, priority="later")
print(f"-- A uuid={ua[:8]} B uuid={ub[:8]}")
a.wait_results(2, 60)
print(f"-- results: {len(a.results())}; alive={a.alive()}")
for r in a.results():
    print(f"   result @ {r['_t']}s -> {str(r.get('result'))[:60]!r} uuid={str(r.get('uuid'))[:8]}")
a.close()
