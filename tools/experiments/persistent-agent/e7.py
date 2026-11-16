import os, sys; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
a = Agent("e7")
a.prompt("Reply with just: hi.")
a.wait_results(1, 40)
def cpu():
    try:
        f = open(f"/proc/{a.p.pid}/stat").read().split()
        return (int(f[13]) + int(f[14])) / 100.0
    except Exception:
        return None
print(f"-- t={a.now():5.1f}s rss={a.rss_mb()}MB cpu={cpu()}s alive={a.alive()}")
for i in range(6):
    time.sleep(30)
    print(f"-- t={a.now():5.1f}s rss={a.rss_mb()}MB cpu={cpu()}s alive={a.alive()}")
a.close()
