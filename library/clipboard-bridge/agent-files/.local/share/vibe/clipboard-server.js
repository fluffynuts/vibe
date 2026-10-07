"use strict";
// vibe: description: The clipboard page's server: a paste target in the user's browser that feeds the agent
// Started by ~/.local/bin/start-clipboard. It serves clipboard-page.html on
// 5390 (published to the host like diffity's 5391) and keeps a history of
// whatever the user pastes, drops or picks there. That history reaches the
// agent two ways:
//
//   - POST /api/claim, called by clipboard-hook on UserPromptSubmit and
//     PostToolUse, hands over everything not yet delivered, so a paste
//     rides along with the user's next message, or reaches a working agent
//     at its next step.
//   - GET /api/clipboard, called by the sbx-clipboard fallback, answers the
//     sandbox's wl-paste/xclip shims with the current item whenever sbx's
//     own host clipboard bridge comes back empty — so Ctrl-V in the agent's
//     terminal attaches it too.
//
// The server is the only writer of the history, so nothing else has to
// worry about two writers racing on index.json.
//
// No template literals: sbx validates every dollar-brace form in a kit's
// file content against its own placeholder list.

const http = require("http");
const fs = require("fs");
const os = require("os");
const path = require("path");

const PORT = Number(process.env.VIBE_CLIPBOARD_PORT || 5390);
const STATE = process.env.VIBE_CLIPBOARD_DIR || "/home/agent/.local/state/vibe/clipboard";
const ITEMS = path.join(STATE, "items");
const INDEX = path.join(STATE, "index.json");
const PAGE = path.join(__dirname, "clipboard-page.html");
const MAX_BYTES = 50 * 1024 * 1024;
// Text short enough to quote inline in the agent's context; anything longer
// is handed over as a file to read.
const INLINE_TEXT = 4000;

const EXTENSIONS = {
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "image/gif": ".gif",
  "image/webp": ".webp",
  "image/svg+xml": ".svg",
  "image/bmp": ".bmp",
  "text/plain": ".txt",
  "text/html": ".html",
  "text/markdown": ".md",
  "application/json": ".json",
  "application/pdf": ".pdf",
};

fs.mkdirSync(ITEMS, { recursive: true });

function load() {
  try {
    const data = JSON.parse(fs.readFileSync(INDEX, "utf8"));
    if (Array.isArray(data.items)) return data;
  } catch (e) { /* a fresh or unreadable history starts empty */ }
  return { nextId: 1, current: null, items: [] };
}

let state = load();

function save() {
  const tmp = INDEX + ".tmp";
  fs.writeFileSync(tmp, JSON.stringify(state, null, 2));
  fs.renameSync(tmp, INDEX);
}

function find(id) {
  return state.items.find(function (i) { return i.id === id; });
}

function baseType(contentType) {
  return String(contentType || "application/octet-stream").split(";")[0].trim().toLowerCase();
}

function kindOf(type) {
  if (type.indexOf("image/") === 0) return "image";
  if (type.indexOf("text/") === 0) return "text";
  return "file";
}

// extensionFor prefers what the type says, then the name the browser gave,
// then nothing at all: the agent reads by path, not by extension.
function extensionFor(type, name) {
  if (EXTENSIONS[type]) return EXTENSIONS[type];
  const ext = path.extname(name || "").toLowerCase();
  return /^\.[a-z0-9]{1,8}$/.test(ext) ? ext : ".bin";
}

function itemPath(item) {
  return path.join(ITEMS, item.file);
}

// describe is an item as the page and the hook see it: everything but where
// it lives on disk, plus the absolute path the agent should read.
function describe(item) {
  return {
    id: item.id,
    kind: item.kind,
    type: item.type,
    name: item.name,
    size: item.size,
    created: item.created,
    delivered: item.delivered,
    path: itemPath(item),
  };
}

function addItem(body, contentType, name) {
  const type = baseType(contentType);
  const id = state.nextId++;
  const item = {
    id: id,
    kind: kindOf(type),
    type: type,
    name: name || "",
    size: body.length,
    created: new Date().toISOString(),
    delivered: null,
    file: id + extensionFor(type, name),
  };
  fs.writeFileSync(itemPath(item), body);
  state.items.push(item);
  state.current = id;
  save();
  return item;
}

function removeItem(item) {
  try { fs.unlinkSync(itemPath(item)); } catch (e) { /* already gone */ }
  state.items = state.items.filter(function (i) { return i !== item; });
  if (state.current === item.id) {
    const last = state.items[state.items.length - 1];
    state.current = last ? last.id : null;
  }
  save();
}

// claim hands over every item not yet delivered, marking them so, with
// short text quoted inline so the agent needn't read a file for it.
function claim() {
  const now = new Date().toISOString();
  const out = [];
  state.items.forEach(function (item) {
    if (item.delivered) return;
    item.delivered = now;
    const d = describe(item);
    if (item.kind === "text" && item.size <= INLINE_TEXT) {
      try { d.text = fs.readFileSync(itemPath(item), "utf8"); } catch (e) { /* hand over the path alone */ }
    }
    out.push(d);
  });
  if (out.length) save();
  return out;
}

function send(res, status, body, headers) {
  res.writeHead(status, Object.assign({ "Cache-Control": "no-store" }, headers || {}));
  res.end(body);
}

function sendJSON(res, status, value) {
  send(res, status, JSON.stringify(value), { "Content-Type": "application/json" });
}

function readBody(req, res, done) {
  const chunks = [];
  let size = 0;
  let failed = false;
  req.on("data", function (chunk) {
    if (failed) return;
    size += chunk.length;
    if (size > MAX_BYTES) {
      failed = true;
      sendJSON(res, 413, { error: "larger than " + MAX_BYTES / 1024 / 1024 + " MB" });
      req.destroy();
      return;
    }
    chunks.push(chunk);
  });
  req.on("end", function () {
    if (!failed) done(Buffer.concat(chunks));
  });
}

// clipboard answers the sbx-clipboard fallback the way sbx's own bridge
// answers the shims: with no type, the current item's type, one per line;
// with a type, the current item's content if it is of that type; otherwise
// nothing. Serving an item this way counts as delivering it, so the hook
// won't hand it over a second time.
function clipboard(res, want) {
  const item = state.current === null ? null : find(state.current);
  if (!item) return send(res, 200, "");
  if (!want) return send(res, 200, item.type + "\n", { "Content-Type": "text/plain" });
  if (baseType(want) !== item.type) return send(res, 200, "");
  let body;
  try { body = fs.readFileSync(itemPath(item)); } catch (e) { return send(res, 200, ""); }
  if (!item.delivered) {
    item.delivered = new Date().toISOString();
    save();
  }
  send(res, 200, body, { "Content-Type": item.type });
}

function handle(req, res) {
  const url = new URL(req.url, "http://localhost");
  const parts = url.pathname.split("/").filter(Boolean);

  if (req.method === "GET" && url.pathname === "/") {
    let page;
    try { page = fs.readFileSync(PAGE); } catch (e) { return send(res, 500, "clipboard page missing: " + PAGE); }
    return send(res, 200, page, { "Content-Type": "text/html; charset=utf-8" });
  }
  if (parts[0] !== "api") return send(res, 404, "not found");

  if (req.method === "GET" && parts[1] === "items" && parts.length === 2) {
    return sendJSON(res, 200, {
      host: os.hostname(),
      current: state.current,
      items: state.items.slice().reverse().map(describe),
    });
  }
  if (req.method === "POST" && parts[1] === "items" && parts.length === 2) {
    let name = "";
    try { name = decodeURIComponent(req.headers["x-filename"] || ""); } catch (e) { /* keep it nameless */ }
    return readBody(req, res, function (body) {
      if (!body.length) return sendJSON(res, 400, { error: "nothing to add" });
      sendJSON(res, 201, describe(addItem(body, req.headers["content-type"], path.basename(name))));
    });
  }
  if (parts[1] === "items" && parts.length >= 3) {
    const item = find(Number(parts[2]));
    if (!item) return sendJSON(res, 404, { error: "no such item" });
    if (req.method === "GET" && parts[3] === "content") {
      let body;
      try { body = fs.readFileSync(itemPath(item)); } catch (e) { return sendJSON(res, 410, { error: "content is gone" }); }
      return send(res, 200, body, { "Content-Type": item.type });
    }
    if (req.method === "POST" && parts[3] === "current") {
      state.current = item.id;
      save();
      return sendJSON(res, 200, describe(item));
    }
    if (req.method === "DELETE" && parts.length === 3) {
      removeItem(item);
      return sendJSON(res, 200, { deleted: item.id });
    }
  }
  if (req.method === "POST" && url.pathname === "/api/claim") {
    return sendJSON(res, 200, { items: claim() });
  }
  if (req.method === "GET" && url.pathname === "/api/clipboard") {
    return clipboard(res, url.searchParams.get("type") || "");
  }
  sendJSON(res, 404, { error: "not found" });
}

http.createServer(function (req, res) {
  try {
    handle(req, res);
  } catch (e) {
    console.error(e);
    if (!res.headersSent) sendJSON(res, 500, { error: String(e.message || e) });
  }
}).listen(PORT, "0.0.0.0", function () {
  console.log("clipboard server listening on " + PORT + ", history in " + STATE);
});
