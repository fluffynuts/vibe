import os, sys, uuid; sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from drive import *
# E10: does the stream pass through unchanged and promptly? Start-up time, the time from writing a prompt to its `queued` frame,
# gaps and bursts between events, and a 150 KB prompt arriving whole.
a = Agent("e10")
u0 = str(uuid.uuid4())
a.prompt("Reply with just: ready.", uuid=u0)
a.wait(lambda evs: any(e.get("subtype") == "init" for e in evs), 60, "first init")
first_init = [e for e in a.events if e.get("subtype") == "init"][0]["_t"]
a.wait_results(1, 60)
print(f"-- first init {first_init}s after the prompt was written; first result at {a.results()[0]['_t']}s")
lat = []
for i in range(1, 7):
    u = str(uuid.uuid4())
    t = a.now()
    a.prompt(f"Reply with just: n{i}", uuid=u)
    ok = a.wait(lambda evs: any(e.get("type") == "command_lifecycle" and e.get("command_uuid") == u and e.get("state") == "queued" for e in evs), 30, "queued frame")
    q = [e for e in a.events if e.get("type") == "command_lifecycle" and e.get("command_uuid") == u and e.get("state") == "queued"]
    lat.append(round(q[0]["_t"] - t, 3) if q else None)
    a.wait_results(1 + i, 30)
    time.sleep(1.5)
print(f"-- seconds from writing a prompt to its `queued` frame: {lat}")
big = "padding " * 19000 + "\nThe word at the very end of this message is ENDMARK. Ignore the padding and reply with just that word."
u = str(uuid.uuid4())
a.prompt(big, uuid=u)
a.wait_results(8, 90)
last = a.results()[-1]
print(f"-- 150 KB prompt ({len(big)} bytes) answered {str(last.get('result'))[:40]!r} (want ENDMARK) at {last['_t']}s")
ts = [e["_t"] for e in a.events]
gaps = [round(b - a_, 2) for a_, b in zip(ts, ts[1:])]
print(f"-- {len(ts)} events; events arriving in the same 0.05 s as the one before: {sum(1 for g in gaps if g < 0.05)}")
print(f"-- agent rss in the sandbox/here: {a.rss_mb()}MB; alive={a.alive()}")
a.close()
