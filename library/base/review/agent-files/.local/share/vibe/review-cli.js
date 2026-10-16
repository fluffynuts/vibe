"use strict";
// vibe: description: vibe-review: the agent's side of the companion page's Review tab
// The agent reviews the changes in its folder with this, and the user reads
// and answers on the companion page's Review tab. The review is kept on the
// host, in a folder (VIBE_REVIEW_DIR) of which vibe mounts two into the
// sandbox: the inbox, where each request is dropped for the page to take
// in (one made while vibe isn't running waits there), and view, read-only,
// with a copy of the review to read. The page's server alone writes the
// review.
//
// No template literals: sbx validates every dollar-brace form in a kit's
// file content against its own placeholder list.

const fs = require("fs");
const path = require("path");
const childProcess = require("child_process");

const USAGE = [
  "usage: vibe-review <command> [options]",
  "",
  "  start [--note TEXT] [--ref BRANCH | --commit SHA | --unpushed]",
  "                      begin a review (of the uncommitted changes, unless --ref,",
  "                      --commit, or --unpushed: the commits not yet pushed to",
  "                      the branch's upstream): the Review tab shows it as in",
  "                      progress",
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
// were made, written whole before it's given a name the page reads. The
// inbox is the host's, mounted: one made here would reach nobody.
function drop(request) {
  const inbox = path.join(reviewDir(), "inbox");
  if (!fs.existsSync(inbox)) {
    fail("the review folder isn't mounted in this sandbox (there's no " + inbox + "), so the Review tab would never see this — " +
      "rebuild the sandbox ('vibe -r' on the host) to mount it", 1);
  }
  const name = String(Date.now()).padStart(15, "0") + "-" +
    process.hrtime.bigint().toString().padStart(20, "0") + "-" + process.pid;
  const tmp = path.join(inbox, name + ".tmp");
  fs.writeFileSync(tmp, JSON.stringify(request));
  const dropped = path.join(inbox, name + ".json");
  fs.renameSync(tmp, dropped);
  return dropped;
}

// startAndWait drops a start, and waits until vibe has taken it in (it does
// every couple of seconds), so what follows (diff, above all) is of the new
// review, as the tab shows it. Taken in is its file gone from the inbox and
// the review saved after (agentAt); the review then is this start's, else
// it says what happened instead.
function startAndWait(request) {
  const before = load();
  const dropped = drop(request);
  const limit = Number(process.env.VIBE_REVIEW_START_WAIT_MS) || 15000;
  const deadline = Date.now() + limit;
  const pause = new Int32Array(new SharedArrayBuffer(4));
  const wait = function (done) {
    while (!done()) {
      if (Date.now() > deadline) return false;
      Atomics.wait(pause, 0, 0, 100);
    }
    return true;
  };
  if (!wait(function () { return !fs.existsSync(dropped); })) {
    fail("the Review tab hasn't taken the start in after " + Math.round(limit / 1000) + "s: is vibe running on the host, " +
      "with its companion page (not --no-companion, nor companion: off)? The start waits in the inbox, and is taken in once it is.", 1);
  }
  let now = load();
  wait(function () { now = load(); return now.agentAt !== before.agentAt; });
  const ours = now.started && now.started !== before.started &&
    (now.ref || undefined) === request.ref && (now.commit || undefined) === request.commit &&
    !!now.unpushed === !!request.unpushed && (now.note || undefined) === request.note;
  if (ours) return;
  const fresh = (now.problems || []).filter(function (p) { return (before.problems || []).indexOf(p) < 0; });
  const refused = fresh.filter(function (p) { return /^start: /.test(p); }).pop();
  if (refused) fail("the Review tab refused the start: " + refused.replace(/^start: /, ""), 1);
  fail("the start was taken in, but the review now isn't it (another was started on the page?): see vibe-review list", 1);
}

// linesOf is lines first to last of file as they read now, for the tab to
// tell when they've changed since; undefined when the file can't be read
// (the comment is still made: it just can't be told to be out of date).
function linesOf(file, first, last) {
  for (const base of [root(), "."]) {
    try {
      const lines = fs.readFileSync(path.resolve(base, file), "utf8").split("\n");
      if (last <= lines.length) return lines.slice(first - 1, last).map(function (l) { return l.replace(/\r$/, ""); }).join("\n");
    } catch (e) { /* tried next */ }
  }
  return undefined;
}

// load is the review, from the view the host keeps of it: no review yet
// is an empty one; one that can't be read is said to be.
function load() {
  const file = path.join(reviewDir(), "view", "review.json");
  let text;
  try {
    text = fs.readFileSync(file, "utf8");
  } catch (e) {
    if (e.code === "ENOENT") return { threads: [] };
    fail("the review can't be read from " + file + ": " + e.message, 1);
  }
  try {
    return JSON.parse(text);
  } catch (e) {
    fail("the review in " + file + " isn't readable JSON: " + e.message, 1);
  }
}

let rootFolder;

// root is the top of the workspace's repository, where the Review tab reads
// it, so paths are the tab's wherever the agent has gone: the workspace
// (VIBE_REVIEW_WORKSPACE) itself when it isn't a repository's, or, in a
// sandbox built before that was set (or on a host whose path isn't one
// here), the folder vibe-review is run in.
function root() {
  if (rootFolder === undefined) {
    // A workspace that isn't here (a Windows host's C:\...) is no use.
    const named = process.env.VIBE_REVIEW_WORKSPACE;
    const workspace = named && fs.existsSync(named) ? named : process.cwd();
    try {
      rootFolder = childProcess.execFileSync("git", ["rev-parse", "--show-toplevel"], { cwd: workspace, encoding: "utf8" }).trim();
    } catch (e) {
      rootFolder = workspace;
    }
  }
  return rootFolder;
}

function git(args) {
  return childProcess.execFileSync("git", ["-c", "core.quotepath=false"].concat(args), { cwd: root(), encoding: "utf8", maxBuffer: 256 * 1024 * 1024 });
}

const DIFF_FLAGS = ["--no-color", "--no-ext-diff", "--no-textconv", "--src-prefix=a/", "--dst-prefix=b/", "-M"];

// diff prints what the Review tab shows: the review's commit, the commits
// not pushed to its upstream, its branch from where they parted, or the
// uncommitted changes, untracked files with them.
function diff() {
  const r = load();
  // A merge commit's change is what it brought in to its first parent, as the tab has it.
  if (r.commit) { process.stdout.write(git(["show", "--format=", "-m", "--first-parent"].concat(DIFF_FLAGS, [r.commit]))); return; }
  if (r.unpushed) {
    const parted = git(["merge-base", r.ref, "HEAD"]).trim();
    process.stdout.write(git(["diff"].concat(DIFF_FLAGS, [parted, "HEAD"])));
    return;
  }
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
  const of = r.commit ? "commit " + r.commit : r.unpushed ? "the commits not pushed to " + r.ref : r.ref ? "the changes against " + r.ref : "the uncommitted changes";
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

// checkStop refuses a stop of the reading order the tab couldn't take in,
// saying which and why: refused there, nobody would see it.
function checkStop(stop, i) {
  const which = "stop " + (i + 1) + " ";
  if (stop === null || typeof stop !== "object" || Array.isArray(stop)) fail(which + "isn't {\"file\": ..., \"line\": N, ...}");
  if (typeof stop.file !== "string" || !stop.file) fail(which + "needs a \"file\"");
  for (const name of ["line", "endLine"]) {
    if (stop[name] !== undefined && !(Number.isInteger(stop[name]) && stop[name] >= 1)) {
      fail(which + "has \"" + name + "\": " + JSON.stringify(stop[name]) + ", which isn't a line number (a whole number from 1)");
    }
  }
  for (const name of ["label", "body"]) {
    if (stop[name] !== undefined && typeof stop[name] !== "string") fail(which + "has a \"" + name + "\" that isn't text");
  }
}

function main(argv) {
  const command = argv[0];
  const rest = argv.slice(1);
  switch (command) {
    case "start": {
      const o = parse(rest, ["unpushed"]);
      if ([o.ref, o.commit, o.unpushed].filter(Boolean).length > 1) fail("one of --ref, --commit or --unpushed, not more");
      if (o.unpushed) {
        let upstream;
        try { upstream = git(["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"]).trim(); } catch (e) {
          fail("--unpushed needs the branch to have an upstream (git push -u, once): this one hasn't");
        }
        startAndWait({ op: "start", note: o.note, ref: upstream, unpushed: true });
        return;
      }
      startAndWait({ op: "start", note: o.note, ref: o.ref, commit: o.commit });
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
      stops.forEach(checkStop);
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
