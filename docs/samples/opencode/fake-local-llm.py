# A stand-in for a local OpenAI-compatible model server (llama.cpp, LM Studio, Ollama...), to try OpenCode's local-model path
# without a model: every chat gets one line back. python3 fake-local-llm.py [port], on 127.0.0.1 (default 8080).
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer

class H(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        sys.stderr.write("fake-llm: " + (fmt % args) + "\n")
    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    def do_GET(self):
        if self.path.rstrip("/").endswith("/models"):
            self._json(200, {"object": "list", "data": [{"id": "qwen3.8-flash-next-gyro-gyro-m", "object": "model"}]})
        else:
            self._json(404, {"error": "not found"})
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0)); req = json.loads(self.rfile.read(n) or b"{}")
        text = "hello from the fake local model"
        if not self.path.rstrip("/").endswith("/chat/completions"):
            return self._json(404, {"error": "not found"})
        if req.get("stream"):
            self.send_response(200); self.send_header("Content-Type", "text/event-stream"); self.end_headers()
            def chunk(delta, finish=None):
                d = {"id": "c1", "object": "chat.completion.chunk", "created": 0, "model": req.get("model", "m"),
                     "choices": [{"index": 0, "delta": delta, "finish_reason": finish}]}
                self.wfile.write(("data: " + json.dumps(d) + "\n\n").encode()); self.wfile.flush()
            chunk({"role": "assistant", "content": ""}); chunk({"content": text}); chunk({}, "stop")
            self.wfile.write(b"data: [DONE]\n\n"); self.wfile.flush()
        else:
            self._json(200, {"id": "c1", "object": "chat.completion", "created": 0, "model": req.get("model", "m"),
                "choices": [{"index": 0, "message": {"role": "assistant", "content": text}, "finish_reason": "stop"}],
                "usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}})

port = int(sys.argv[1]) if len(sys.argv) > 1 else 8080
HTTPServer(("127.0.0.1", port), H).serve_forever()
