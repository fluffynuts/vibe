"use strict";
// vibe: description: Claude Code hook logic behind diffity-url-hook
// Claude Code hook (PostToolUse and Stop), run through
// ~/.local/bin/diffity-url-hook and registered for every session in
// /etc/claude-code/managed-settings.d by the diffity feature's install step.
//
// The agent is told to hand over diffity's host URL, but the diffity skills
// themselves say "running at http://localhost:5391" or "check your browser",
// and in the moment the skill wins: the user gets the sandbox's own port,
// which their browser can't reach, or no URL at all. So:
//
//   - PostToolUse: the first time a turn touches diffity, tell the agent the
//     URL the user can actually open (from diffity-url, which vibe keeps in
//     step with sbx's live port mapping).
//   - Stop: a turn that used diffity may not end until the reply gives that
//     URL, and quotes no other localhost port — unless diffity says no
//     viewer is running, so there's nothing to open. It is asked once per turn —
//     stop_hook_active — so a disagreement can never loop.
//
// Whatever goes wrong in here, the hook stays quiet and exits 0: a broken
// check must never get in the way of the agent.
//
// No template literals: sbx validates every dollar-brace form in a kit's file
// content against its own placeholder list.

const fs = require("fs");
const path = require("path");
const os = require("os");
const { spawnSync } = require("child_process");

// Overridable only so the hook can be tested outside a sandbox.
const DIFFITY_URL = process.env.DIFFITY_URL_COMMAND || "/home/agent/.local/bin/diffity-url";

// A diffity command in a shell line: "diffity ..." or "npx diffity ...",
// but not diffity-url, which only prints the URL.
const DIFFITY_COMMAND = /(^|[\s;&|(`])(npx\s+(-y\s+)?)?diffity(?![\w-])/;

// A slash command, as it is recorded in the prompt that ran it.
const DIFFITY_SLASH_COMMAND = /<command-name>\/?diffity-[\w-]+<\/command-name>|^\s*\/diffity-[\w-]+/;

// Any URL on this machine: the only ports a reply could quote wrongly.
const LOCAL_URL = /\b(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]):(\d+)/g;

function isDiffityCommand(command) {
  return typeof command === "string" && DIFFITY_COMMAND.test(command);
}

function isDiffitySkill(name) {
  return typeof name === "string" && /^diffity-/.test(name.replace(/^.*:/, ""));
}

function isDiffityToolUse(name, input) {
  input = input || {};
  if (name === "Bash") return isDiffityCommand(input.command);
  if (name === "Skill") return isDiffitySkill(input.skill || input.command || input.name);
  return false;
}

// diffityURL runs diffity-url for the base URL, and whatever it warned.
function diffityURL() {
  const r = spawnSync(DIFFITY_URL, [], {
    encoding: "utf8",
    timeout: 5000,
    // the base URL is for briefing the agent, not one handed to the user
    env: Object.assign({}, process.env, { VIBE_DIFFITY_URL_NO_RECORD: "1" }),
  });
  const url = (r.stdout || "").trim().split("\n")[0];
  if (r.status !== 0 || !url) return null;
  // the origin alone: diffity-url's answer may carry /diff and a ref
  const base = url.replace(/^(https?:\/\/[^/?#]+).*$/, "$1");
  const m = /:(\d+)$/.exec(base);
  return { base: base, port: m ? m[1] : "", warning: (r.stderr || "").trim() };
}

// noSessionRunning reports whether diffity says, for certain, that no
// viewer is running — in which case there is no URL worth handing over. A
// diffity that can't be asked counts as running: better a URL too many.
function noSessionRunning(cwd) {
  const r = spawnSync(process.env.DIFFITY_COMMAND || "diffity", ["list", "--json"],
    { encoding: "utf8", timeout: 5000, cwd: cwd || undefined });
  if (r.status !== 0) return false;
  try {
    const sessions = JSON.parse(r.stdout);
    return Array.isArray(sessions) && sessions.length === 0;
  } catch (e) {
    return false;
  }
}

function readTranscript(file) {
  try {
    return fs.readFileSync(file, "utf8").split("\n").filter(Boolean).map(function (line) {
      try { return JSON.parse(line); } catch (e) { return null; }
    }).filter(Boolean);
  } catch (e) {
    return [];
  }
}

// promptText is the text of a real user prompt, or null for anything else
// (a tool result, an injected meta message, a non-user entry).
function promptText(entry) {
  if (entry.type !== "user" || entry.isMeta || !entry.message) return null;
  const content = entry.message.content;
  if (typeof content === "string") return content;
  if (!Array.isArray(content) || content.some(function (b) { return b.type === "tool_result"; })) return null;
  return content.filter(function (b) { return b.type === "text"; }).map(function (b) { return b.text; }).join("\n");
}

// currentTurn is every transcript entry from the last real prompt on.
function currentTurn(entries) {
  for (let i = entries.length - 1; i >= 0; i--) {
    if (promptText(entries[i]) !== null) return entries.slice(i);
  }
  return entries;
}

function turnUsedDiffity(turn) {
  return turn.some(function (entry) {
    const text = promptText(entry);
    if (text !== null && DIFFITY_SLASH_COMMAND.test(text)) return true;
    const content = entry.type === "assistant" && entry.message && entry.message.content;
    return Array.isArray(content) && content.some(function (b) {
      return b.type === "tool_use" && isDiffityToolUse(b.name, b.input);
    });
  });
}

function lastAssistantText(turn) {
  for (let i = turn.length - 1; i >= 0; i--) {
    const content = turn[i].type === "assistant" && turn[i].message && turn[i].message.content;
    if (!Array.isArray(content)) continue;
    const text = content.filter(function (b) { return b.type === "text"; }).map(function (b) { return b.text; }).join("\n");
    if (text) return text;
  }
  return "";
}

function warningNote(u) {
  if (!u.warning) return "";
  return " diffity-url also warned: \"" + u.warning.replace(/\s+/g, " ") +
    "\" — pass that on, rather than presenting the URL as certain.";
}

function howToGiveTheURL(u) {
  return "Give the user the full URL, built on " + u.base + ": for a diff or review, run `diffity-url <ref>` " +
    "with the same ref the diffity command used (no argument when it had none) and quote exactly what it " +
    "prints; for a tour, " + u.base + "/tour/<tour-id>. Never quote localhost:5391 or the port `diffity list` " +
    "reports: that is the sandbox's side of the port mapping, and the user's browser can't reach it.";
}

// onPostToolUse briefs the agent, once per turn, when it first touches
// diffity.
function onPostToolUse(input) {
  if (!isDiffityToolUse(input.tool_name, input.tool_input)) return null;
  const marker = path.join(os.tmpdir(), "diffity-url-hook-" +
    String(input.session_id || "session").replace(/[^\w-]/g, "") + "-" +
    String(input.prompt_id || "turn").replace(/[^\w-]/g, ""));
  if (fs.existsSync(marker)) return null;
  try { fs.writeFileSync(marker, ""); } catch (e) { /* brief again next time, then */ }
  const u = diffityURL();
  if (!u) return null;
  return {
    hookSpecificOutput: {
      hookEventName: "PostToolUse",
      additionalContext: "The user opens diffity in their own browser at " + u.base +
        " — the host port vibe confirmed for this sandbox. " + howToGiveTheURL(u) +
        " Whenever you report on this diffity session or review — including after resolving comments — " +
        "end with that URL." + warningNote(u),
    },
  };
}

// onStop holds a turn that used diffity until its reply carries the URL.
function onStop(input) {
  if (input.stop_hook_active) return null;
  const turn = currentTurn(readTranscript(input.transcript_path));
  if (!turnUsedDiffity(turn) || noSessionRunning(input.cwd)) return null;
  const u = diffityURL();
  if (!u) return null;

  const reply = typeof input.last_assistant_message === "string" && input.last_assistant_message
    ? input.last_assistant_message
    : lastAssistantText(turn);
  const wrong = [];
  let m;
  LOCAL_URL.lastIndex = 0;
  while ((m = LOCAL_URL.exec(reply)) !== null) {
    if (m[1] !== u.port && wrong.indexOf(m[0]) < 0) wrong.push(m[0]);
  }
  const givesURL = new RegExp(u.base.replace(/[.*+?^()|[\]\\{}$]/g, "\\$&") + "(?!\\d)").test(reply);
  if (givesURL && wrong.length === 0) return null;

  const problem = wrong.length > 0
    ? "your reply quotes " + wrong.join(", ") + ", which the user's browser can't reach."
    : "your reply doesn't give the user the URL to open it at.";
  return {
    decision: "block",
    reason: "This turn used diffity, but " + problem + " " + howToGiveTheURL(u) +
      " Finish your reply with it now, without repeating the rest." + warningNote(u),
  };
}

function main() {
  let input;
  try {
    input = JSON.parse(fs.readFileSync(0, "utf8"));
  } catch (e) {
    return;
  }
  let out = null;
  if (input.hook_event_name === "PostToolUse") out = onPostToolUse(input);
  else if (input.hook_event_name === "Stop") out = onStop(input);
  if (out) process.stdout.write(JSON.stringify(out));
}

if (require.main === module) {
  try { main(); } catch (e) { /* never in the agent's way */ }
} else {
  module.exports = {
    isDiffityCommand: isDiffityCommand,
    isDiffityToolUse: isDiffityToolUse,
    currentTurn: currentTurn,
    turnUsedDiffity: turnUsedDiffity,
    onStop: onStop,
    onPostToolUse: onPostToolUse,
  };
}
