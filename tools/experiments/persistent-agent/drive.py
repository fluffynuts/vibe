"""Drives one streaming `claude` (stream-json in and out) and logs every event with a time, for the persistent-agent experiments."""
import json, os, subprocess, sys, threading, time, queue

MODEL = os.environ.get("EXP_MODEL", "claude-haiku-5-5")
OUT = os.environ.get("EXP_OUT", "/tmp/exp-out")
CWD = os.environ.get("EXP_CWD", "/tmp/exp-cwd")
os.makedirs(OUT, exist_ok=True)
os.makedirs(CWD, exist_ok=True)

# EXP_SBX names a sandbox: the agent is then started as vibe starts one (sbx exec -i, the login-shell script, the pid-file wrapper),
# in EXP_WORKSPACE inside it, instead of directly. Without it the agent runs here.
SBX = os.environ.get("EXP_SBX")
WORKSPACE = os.environ.get("EXP_WORKSPACE", "/tmp")
AS_AGENT = 'if [ "$(id -u)" = 0 ]; then exec su - "$(id -un 1000 2>/dev/null || echo agent)" -c "$1"; else exec bash -lc "$1"; fi'
# Copied from src/vibe/conversation.go (stopTurnScript, turnStopWithin): what vibe runs to end a turn.
STOP_SCRIPT = """f=%s
touch "$f.stop"
[ -f "$f" ] || exit 0
tree() { for c in $(pgrep -P "$1"); do tree "$c"; done; echo "$1"; }
alive() { kill -0 "$1" 2>/dev/null && ! grep -q ') Z ' "/proc/$1/stat" 2>/dev/null; }
pids=$(tree "$(cat "$f")")
kill -TERM $pids 2>/dev/null
i=0
while [ "$i" -lt 50 ]; do
  left=
  for p in $pids; do alive "$p" && left="$left $p"; done
  [ -z "$left" ] && { rm -f "$f" "$f.stop"; exit 0; }
  sleep 0.1
  i=$((i + 1))
done
kill -KILL $left 2>/dev/null
rm -f "$f" "$f.stop"
echo "killed what was still running 5s after being asked to stop:$left"
"""

def q(s):
    return "'" + s.replace("'", "'\\''") + "'"

def sbx_exec(*args, input=None, timeout=60):
    return subprocess.run(["sbx", "exec", SBX, "--", *args], capture_output=True, text=True, input=input, timeout=timeout)

def summarize(ev):
    t = ev.get("type")
    st = ev.get("subtype")
    if t == "system":
        keys = {k: ev[k] for k in ev if k in ("subtype", "task_id", "status", "session_id", "model", "summary", "description", "output_file", "reason", "source") and k != "subtype"}
        return f"system/{st} {json.dumps(keys)[:200]}"
    if t == "assistant":
        parts = []
        for b in ev.get("message", {}).get("content", []):
            if b.get("type") == "text":
                parts.append("text:" + b["text"][:80].replace("\n", " "))
            elif b.get("type") == "tool_use":
                inp = b.get("input", {})
                parts.append("tool_use:" + b.get("name", "") + " " + json.dumps(inp)[:100])
            else:
                parts.append(b.get("type", "?"))
        return "assistant " + " | ".join(parts)
    if t == "user":
        c = ev.get("message", {}).get("content")
        if isinstance(c, list):
            parts = []
            for b in c:
                if b.get("type") == "tool_result":
                    parts.append("tool_result:" + str(b.get("content"))[:80].replace("\n", " "))
                else:
                    parts.append(b.get("type", "?"))
            return "user " + " | ".join(parts)
        return "user text:" + str(c)[:100].replace("\n", " ") + ("  [replay]" if ev.get("isReplay") else "")
    if t == "result":
        return f"RESULT {st} is_error={ev.get('is_error')} turns={ev.get('num_turns')} dur={ev.get('duration_ms')}ms result={str(ev.get('result'))[:80]!r}"
    if t in ("control_response", "control_request", "control_cancel_request"):
        return f"{t} {json.dumps(ev)[:260]}"
    return f"{t}/{st} {json.dumps(ev)[:200]}"

class Agent:
    def __init__(self, name, extra=()):
        self.t0 = time.time()
        self.name = name
        self.log = open(f"{OUT}/{name}.jsonl", "w")
        words = ["claude", "-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
                 "--permission-mode", "bypassPermissions", "--model", MODEL, *extra]
        self.pidfile = None
        if SBX:
            # As vibe's conversationCommand and wrapTurn build it (no --strict-mcp-config: vibe does not use it).
            self.pidfile = "/tmp/vibe-exp-" + name + "-" + str(os.getpid()) + ".pid"
            pid, stop = q(self.pidfile), q(self.pidfile + ".stop")
            inner = "cd " + q(WORKSPACE) + " && " + " ".join(q(w) for w in words)
            wrapped = "echo $$ > " + pid + "; trap " + q("rm -f " + q(self.pidfile) + " " + q(self.pidfile + ".stop")) + " EXIT; [ -e " + stop + " ] && exit 0; " + inner
            args = ["sbx", "exec", "-i", SBX, "--", "sh", "-c", AS_AGENT, "converse", wrapped]
        else:
            args = words[:9] + ["--strict-mcp-config"] + words[9:]
        self.p = subprocess.Popen(args, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, cwd=CWD, bufsize=1)
        self.events = []
        self.q = queue.Queue()
        self.lock = threading.Lock()
        threading.Thread(target=self._read, daemon=True).start()
        threading.Thread(target=self._err, daemon=True).start()

    def now(self):
        return time.time() - self.t0

    def _read(self):
        for line in self.p.stdout:
            line = line.strip()
            if not line:
                continue
            try:
                ev = json.loads(line)
            except Exception:
                ev = {"type": "unparsed", "raw": line[:200]}
            ev["_t"] = round(self.now(), 2)
            with self.lock:
                self.events.append(ev)
            self.log.write(json.dumps(ev) + "\n"); self.log.flush()
            if ev.get("type") == "system" and ev.get("subtype") == "init":
                print(f"[{ev['_t']:6.2f}] system/init session={ev.get('session_id')} model={ev.get('model')}", flush=True)
            else:
                print(f"[{ev['_t']:6.2f}] {summarize(ev)}", flush=True)
        print(f"[{self.now():6.2f}] <stdout closed, exit={self.p.poll()}>", flush=True)

    def _err(self):
        for line in self.p.stderr:
            print(f"[{self.now():6.2f}] STDERR {line.rstrip()[:200]}", flush=True)

    def send(self, obj, label=""):
        print(f"[{self.now():6.2f}] >>> {label or json.dumps(obj)[:160]}", flush=True)
        self.p.stdin.write(json.dumps(obj) + "\n"); self.p.stdin.flush()

    def prompt(self, text, **extra):
        msg = {"type": "user", "message": {"role": "user", "content": text}}
        msg.update(extra)
        self.send(msg, "prompt: " + text[:90])

    def results(self):
        with self.lock:
            return [e for e in self.events if e.get("type") == "result"]

    def wait(self, pred, timeout, what=""):
        end = time.time() + timeout
        while time.time() < end:
            with self.lock:
                if pred(self.events):
                    return True
            time.sleep(0.1)
        print(f"[{self.now():6.2f}] !!! timed out waiting for {what}", flush=True)
        return False

    def wait_results(self, n, timeout):
        return self.wait(lambda evs: sum(1 for e in evs if e.get("type") == "result") >= n, timeout, f"{n} results")

    def in_sandbox(self, script):
        """Runs script in the sandbox (sbx mode) and gives its stdout."""
        return sbx_exec("sh", "-c", script).stdout.strip()

    def agent_pid_in_sandbox(self):
        """The pid the wrapper recorded, if that process is alive in the sandbox (sbx mode); else None."""
        out = self.in_sandbox('f=%s; [ -f "$f" ] && p=$(cat "$f") && kill -0 "$p" 2>/dev/null && echo "$p"' % q(self.pidfile))
        return out or None

    def hard_stop(self):
        """vibe's own stopTurnScript, run in the sandbox (sbx mode): what the page's Stop does today."""
        return self.in_sandbox(STOP_SCRIPT % q(self.pidfile))

    def rss_mb(self):
        if SBX:
            pid = self.agent_pid_in_sandbox()
            if not pid:
                return None
            kids = self.in_sandbox('for p in $(pgrep -P %s) %s; do grep VmRSS /proc/$p/status 2>/dev/null; done' % (pid, pid))
            vals = [int(l.split()[1]) // 1024 for l in kids.splitlines() if l.startswith("VmRSS")]
            return max(vals) if vals else None
        try:
            for l in open(f"/proc/{self.p.pid}/status"):
                if l.startswith("VmRSS"):
                    return int(l.split()[1]) // 1024
        except Exception:
            return None

    def alive(self):
        return self.p.poll() is None

    def close(self, grace=5):
        try:
            self.p.stdin.close()
        except Exception:
            pass
        end = time.time() + grace
        while time.time() < end and self.alive():
            time.sleep(0.1)
        if self.alive():
            self.p.terminate()
        self.log.close()
