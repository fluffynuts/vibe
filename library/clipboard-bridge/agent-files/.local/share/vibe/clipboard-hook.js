"use strict";
// vibe: description: Claude Code hook logic behind clipboard-hook
// Claude Code hook (UserPromptSubmit and PostToolUse), run through
// ~/.local/bin/clipboard-hook and registered for every session in
// /etc/claude-code/managed-settings.d by the clipboard feature's install
// step.
//
// Nothing can push a message into a Claude Code session from outside, so
// what the user pastes on the clipboard page waits for the next moment a
// hook runs: their next prompt, or — while the agent is working — its next
// tool call. Either way the hook claims everything not yet delivered from
// the clipboard server and adds it to the agent's context: short text
// inline, anything else as a path to read.
//
// Whatever goes wrong in here, the hook stays quiet and exits 0: a broken
// clipboard must never get in the way of the agent. It is quick to give up,
// too, since PostToolUse runs it after every tool call.
//
// No template literals: sbx validates every dollar-brace form in a kit's
// file content against its own placeholder list.

const fs = require("fs");
const http = require("http");

// Overridable only so the hook can be tested outside a sandbox.
const SERVER = process.env.VIBE_CLIPBOARD_SERVER || "http://127.0.0.1:5390";

function claim(done) {
  const req = http.request(SERVER + "/api/claim", { method: "POST", timeout: 1500 }, function (res) {
    const chunks = [];
    res.on("data", function (c) { chunks.push(c); });
    res.on("end", function () {
      try {
        done(JSON.parse(Buffer.concat(chunks).toString("utf8")).items || []);
      } catch (e) {
        done([]);
      }
    });
  });
  req.on("timeout", function () { req.destroy(); });
  req.on("error", function () { done([]); });
  req.end();
}

function size(n) {
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + " KB";
  return (n / 1024 / 1024).toFixed(1) + " MB";
}

// fence picks a code fence longer than any run of backticks in the text, so
// quoted text can never close it early.
function fence(text) {
  const runs = text.match(/`+/g) || [];
  const longest = runs.reduce(function (n, r) { return Math.max(n, r.length); }, 0);
  return "`".repeat(Math.max(3, longest + 1));
}

function describe(item) {
  const what = item.name ? "\"" + item.name + "\" " : "";
  if (typeof item.text === "string") {
    const f = fence(item.text);
    return "- text " + what + "(" + item.path + "):\n" + f + "\n" + item.text + "\n" + f;
  }
  const how = item.kind === "image"
    ? "look at it with the Read tool"
    : "read it if it's relevant";
  return "- " + item.kind + " " + what + "at " + item.path + " (" + item.type + ", " + size(item.size) + ") — " + how;
}

function context(items, event) {
  const when = event === "UserPromptSubmit"
    ? "along with this message"
    : "while you were working";
  return "The user sent " + (items.length === 1 ? "this" : "these") + " to you through the vibe clipboard page " +
    when + ". Take " + (items.length === 1 ? "it" : "them") + " into account as part of what they're asking:\n" +
    items.map(describe).join("\n");
}

function main() {
  let input;
  try {
    input = JSON.parse(fs.readFileSync(0, "utf8"));
  } catch (e) {
    return;
  }
  const event = input.hook_event_name;
  if (event !== "UserPromptSubmit" && event !== "PostToolUse") return;
  claim(function (items) {
    if (!items.length) return;
    process.stdout.write(JSON.stringify({
      hookSpecificOutput: { hookEventName: event, additionalContext: context(items, event) },
    }));
  });
}

if (require.main === module) {
  try { main(); } catch (e) { /* never in the agent's way */ }
} else {
  module.exports = { context: context, fence: fence };
}
