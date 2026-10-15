"use strict";
// vibe: description: vibe-review: the agent's side of the companion page's Review tab
// The agent reviews the changes in its folder with this, and the user reads
// and answers on the companion page's Review tab. The review is kept on the
// host, in a folder vibe mounts into the sandbox (VIBE_REVIEW_DIR); the
// page's server is its only writer, so this never writes the review itself:
// each request is dropped in the folder's inbox, which the page takes in.
// A request made while vibe isn't running waits there.
//
// No template literals: sbx validates every dollar-brace form in a kit's
// file content against its own placeholder list.

const fs = require("fs");
const path = require("path");
const childProcess = require("child_process");

const USAGE = [
  "usage: vibe-review <command> [options]",
  "",
  "  start [--note TEXT] [--ref BRANCH | --commit SHA]",
  "                      begin a review (of the uncommitted changes, unless --ref",
  "                      or --commit): the Review tab shows it as in progress",
  "  diff                the diff the Review tab shows, for the review started",
  "  comment --file PATH --line N [--end-line N] [--side new|old] BODY",
  "                      comment on lines of the diff (the new file's, unless",
  "                      --side old for deleted lines)",
  "  general BODY        a comment on the whole review",
  "  reply --thread N BODY",
  "  resolve --thread N [BODY]",
  "  dismiss --thread N [BODY]",
  "  order               the reading order, as JSON on stdin:",
  "                      [{\"file\": ..., \"line\": N, \"label\": ..., \"body\": ...}]",
  "  done                the review is finished",
  "  list [--json]       the review as it stands, with the user's replies",
  "",
  "  BODY is --body TEXT, or --body-file PATH (- for stdin): Markdown.",
].join("\n");

function fail(message, code) {
  process.stderr.write("vibe-review: " + message + "\n");
  process.exit(code === undefined ? 2 : code);
}

function reviewDir() {
  const dir = process.env.VIBE_REVIEW_DIR;
  if (!dir) {
    fail("VIBE_REVIEW_DIR isn't set: this sandbox was built before the Review tab, so it has no review folder yet — " +
      "rebuild it ('vibe -r' on the host) to get one", 1);
  }
  return dir;
}

// parse reads options (--name value) from args; flags are names that take
// no value.
function parse(args, flags) {
  const opts = {};
  for (let i = 0; i < args.length; i++) {
    const a = args[i];
    if (!a.startsWith("--")) fail("unexpected " + JSON.stringify(a) + "\n\n" + USAGE);
    const name = a.slice(2);
    if (flags && flags.indexOf(name) >= 0) { opts[name] = true; continue; }
    if (i + 1 >= args.length) fail(a + " needs a value");
    opts[name] = args[++i];
  }
  return opts;
}

function number(opts, name, required) {
  if (opts[name] === undefined) {
    if (required) fail("--" + name + " is needed");
    return undefined;
  }
  const n = Number(opts[name]);
  if (!Number.isInteger(n) || n < 1) fail("--" + name + " is a line or thread number, not " + JSON.stringify(opts[name]));
  return n;
}

function body(opts, required) {
  let text;
  if (opts["body-file"] !== undefined) {
    text = fs.readFileSync(opts["body-file"] === "-" ? 0 : opts["body-file"], "utf8");
  } else {
    text = opts.body;
  }
  if (text !== undefined) text = text.replace(/\s+$/, "");
  if (required && !text) fail("say something: --body TEXT, or --body-file PATH (- for stdin)");
  return text || undefined;
}

// drop puts request in the inbox, named so requests sort in the order they
// were made, written whole before it's given a name the page reads.
function drop(request) {
  const inbox = path.join(reviewDir(), "inbox");
  fs.mkdirSync(inbox, { recursive: true });
  const name = String(Date.now()).padStart(15, "0") + "-" +
    process.hrtime.bigint().toString().padStart(20, "0") + "-" + process.pid;
  const tmp = path.join(inbox, name + ".tmp");
  fs.writeFileSync(tmp, JSON.stringify(request));
  fs.renameSync(tmp, path.join(inbox, name + ".json"));
}

// linesOf is lines first to last of file as they read now, for the tab to
// tell when they've changed since; undefined when the file can't be read
// (the comment is still made: it just can't be told to be out of date).
function linesOf(file, first, last) {
  let root = ".";
  try { root = git(["rev-parse", "--show-toplevel"]).trim(); } catch (e) { /* not a repository: relative to here */ }
  for (const base of [root, "."]) {
    try {
      const lines = fs.readFileSync(path.resolve(base, file), "utf8").split("\n");
      if (last <= lines.length) return lines.slice(first - 1, last).map(function (l) { return l.replace(/\r$/, ""); }).join("\n");
    } catch (e) { /* tried next */ }
  }
  return undefined;
}

function load() {
  try {
    return JSON.parse(fs.readFileSync(path.join(reviewDir(), "review.json"), "utf8"));
  } catch (e) {
    return { threads: [] };
  }
}

function git(args) {
  return childProcess.execFileSync("git", ["-c", "core.quotepath=false"].concat(args), { encoding: "utf8", maxBuffer: 256 * 1024 * 1024 });
}

const DIFF_FLAGS = ["--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/", "-M"];

// diff prints what the Review tab shows: the review's commit, its branch
// from where they parted, or the uncommitted changes, untracked files with
// them.
function diff() {
  const r = load();
  if (r.commit) { process.stdout.write(git(["show", "--format="].concat(DIFF_FLAGS, [r.commit]))); return; }
  let base = "HEAD";
  if (r.ref) base = git(["merge-base", r.ref, "HEAD"]).trim();
  else {
    try { git(["rev-parse", "--verify", "--quiet", "HEAD"]); } catch (e) { base = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"; }
  }
  process.stdout.write(git(["diff"].concat(DIFF_FLAGS, [base])));
  const untracked = git(["ls-files", "--others", "--exclude-standard", "-z"]).split("\0").filter(Boolean);
  for (const f of untracked) {
    try {
      git(["diff", "--no-index"].concat(DIFF_FLAGS, ["/dev/null", f]));
    } catch (e) {
      // git diff --no-index exits 1 when there are differences: that's its output.
      if (e.stdout) process.stdout.write(e.stdout);
    }
  }
}

function list(json) {
  const r = load();
  if (json) { process.stdout.write(JSON.stringify(r, null, 2) + "\n"); return; }
  const of = r.commit ? "commit " + r.commit : r.ref ? "against " + r.ref : "the uncommitted changes";
  process.stdout.write("review of " + of + (r.note ? ": " + r.note : "") + (r.state ? " (" + r.state + ")" : "") + "\n");
  for (const t of r.threads || []) {
    const where = t.file ? t.file + ":" + t.line + (t.endLine > t.line ? "-" + t.endLine : "") + (t.side === "old" ? " (old)" : "") : "general";
    process.stdout.write("\n#" + t.id + " " + t.status + "  " + where + "\n");
    for (const c of t.comments || []) {
      process.stdout.write("  " + c.author + ": " + c.body.replace(/\n/g, "\n    ") + "\n");
    }
  }
  for (const p of r.problems || []) process.stdout.write("\nproblem: " + p + "\n");
}

function main(argv) {
  const command = argv[0];
  const rest = argv.slice(1);
  switch (command) {
    case "start": {
      const o = parse(rest);
      if (o.ref && o.commit) fail("--ref or --commit, not both");
      drop({ op: "start", note: o.note, ref: o.ref, commit: o.commit });
      return;
    }
    case "comment": {
      const o = parse(rest);
      if (!o.file) fail("--file is needed");
      const side = o.side || "new";
      if (side !== "new" && side !== "old") fail("--side is new or old, not " + JSON.stringify(o.side));
      const line = number(o, "line", true);
      const endLine = number(o, "end-line", false);
      drop({ op: "comment", file: o.file, line: line, endLine: endLine, side: side, code: side === "new" ? linesOf(o.file, line, endLine || line) : undefined, body: body(o, true) });
      return;
    }
    case "general":
      drop({ op: "general", body: body(parse(rest), true) });
      return;
    case "reply":
    case "resolve":
    case "dismiss": {
      const o = parse(rest);
      drop({ op: command, thread: number(o, "thread", true), body: body(o, command === "reply") });
      return;
    }
    case "order": {
      let stops;
      try { stops = JSON.parse(fs.readFileSync(0, "utf8")); } catch (e) { fail("order reads its stops as JSON on stdin: " + e.message); }
      if (!Array.isArray(stops)) fail("order reads a JSON list of stops on stdin");
      drop({ op: "order", stops: stops });
      return;
    }
    case "done":
      reviewDir();
      drop({ op: "done" });
      return;
    case "diff":
      reviewDir();
      diff();
      return;
    case "list":
      list(!!parse(rest, ["json"]).json);
      return;
    default:
      fail(USAGE);
  }
}

main(process.argv.slice(2));
