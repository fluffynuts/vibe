import os, sys; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
a = Agent("e1b")
a.prompt("Use the Bash tool with run_in_background=true to run exactly: sleep 12; echo BGDONE. Do NOT wait for it, do not poll it, do not run anything else. Reply with just the word: started.")
a.wait_results(1, 60)
print(f"-- first result at {a.results()[0]['_t']}s; alive={a.alive()} rss={a.rss_mb()}MB; no prompt will be sent now")
a.wait_results(2, 45)
print(f"-- results so far: {len(a.results())}; alive={a.alive()}")
a.close()
