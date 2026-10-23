# Quality of life: questions that stand out, a power menu, and a way to report an issue (planned, not built)

Raised 2026-10-23. Two small things, bundled: they are each a day's work, and neither needs its own plan to decide.

## 1. A question asked on the command line stands out

**Now:** a question is plain text among vibe's other lines. `confirmDefault` prints `vibe: <question> [Y/n] ` (`src/vibe/prompts.go`), `asking` prints it with `note("%s", question)` before a picker (`chooseFrom`, `filterFrom`, the checkbox prompts), `waitForEnter` and the token prompts in `gittoken.go`, and the exit confirmation in `memories.go` print their own. Something vibe asks scrolls past among the notes around it.

**Proposed:** the question itself, and nothing else of the prompt, is bright and bold: not the `vibe: ` prefix, not the `[Y/n]` hint, not the list a picker draws (`prompt.Select`, `prompt.Filter`, the checkboxes), not the answer typed.

- One function says it: `askText(question)` (or the question's `paint("1", …)`), built on the existing `paint` in `gittoken.go` (`ESC[…m`, off when `NO_COLOR` is set or stderr isn't a terminal); `prompt.EnableVT` already turns escapes on for Windows. Every place that prints a question goes through it.
- Bold plus the terminal's own bright is enough: no colour is chosen, so a theme's palette decides how it looks, and it survives colour-blindness and light and dark backgrounds.
- **Mechanical, so it stays so:** a test that reads the Go source and fails for any `Fprint*(os.Stderr, "vibe: …` that is a prompt and doesn't go through `askText`, as `guard_test.go` does for routes. A new prompt that forgets is caught in CI, not by someone squinting at a terminal.
- **Tests:** with a terminal and colour, the question is wrapped in the bold escape and its reset, and the hint and the options aren't; with `NO_COLOR`, or output that isn't a terminal (a pipe, a log file: the headless vibe's log), no escape appears at all; the text is the same either way (so a log reads as before); each of the callers above is covered once.
- **Not decided:** whether the headline of a multi-line question ("Remove sandbox 'x' (workspace …)?") is all bold, or its first line. The user's wording: "just the question itself".

## 2. A power menu on the page: stop, restart, and a way to report an issue

**What is wanted:** a power button (a circle with a line through its top: the power switch's symbol) in the page's top bar, opening a menu: **Stop this session**, **Restart**, and, after a divider, **Report a bug**, **Request a feature**, **Something else**. The aim of restart is that the user no longer has to go to a console to `vibe --restart`.

### Stop and restart

**Now:** the Admin tab's actions (`AdminStart`, `AdminStop`, `AdminReInit`, `AdminReCompose`, `AdminRestore`, `AdminMemories` in `internal/companion/admin.go`) act on the *sandbox* while the vibe, and its page, carry on (the session "hold"). Nothing on the page ends or restarts the **vibe** itself; `vibe --restart` does (`restartFolder` in `src/vibe/restart.go`: it ends the vibe that holds the folder, saving the agent's memories, asking first if that interrupts an agent mid-turn or someone's terminal session, and the new one starts in the same mode). And `vibe --stop` ends the vibe and stops the sandbox.

**Proposed**

- **Stop this session** is `--stop` from the page: ends this vibe and stops the sandbox. It asks first when the agent is mid-turn (the page already knows: `convo.running`), in a dialog, never `confirm()`.
- **Restart** is `--restart` from the page: ends this vibe and starts another in the same mode on the same port (`CompanionPort` is recorded so a bookmark and the user's `ssh -L` still work), which the page notices when it can reach the host again.
- **A dialog shows what happened:** the operation's output, streamed as it comes, with a clear end ("restarted: the page is back", or what failed and what to do). The user must be able to see it went right, as at the console. The old process is the one being ended, so the output has to be written where the new process can show it: the headless vibe's log (`headlessLog`, `~/.vibe/logs/<name>.log`) is already that file; the new vibe serves its tail (`GET /api/restart-log`, from a byte offset) from its first moment, so the page has something to show while the sandbox is still starting.
- **The questions the console would ask must be answerable on the page.** `vibe --restart` can stop at an out-of-date profile or sandbox (the auto-update's countdown in `detachHeadless`, the re-compose prompt), at "restart anyway?", and at the git author question. A restart from the page has no terminal for them, so each becomes a dialog on the page, with the same two answers as at the console: **accept** the re-composition, or **bypass** it. The new vibe starts its page *before* it asks, in a "starting" state that holds the questions (the same pattern as the git author desk: `State()` with a phase, `Decide(answer)`), and goes on once they're answered. `-f` doesn't apply: a person is there.
- **What it must not do:** end the vibe with the user's work lost without saying so. Mid-turn is asked about; a rebuild under way (`Work != ""` in the claim) is refused, with the reason.
- **Discovery first:** which process spawns the new vibe (the old one, detached, before it exits, as `detachHeadless` does for `--headless`) and what exactly crosses over (the token stays; the page's `pageVersion` changes with a new build, which the page already notices as "this page is out of date"). The case to prove with a real run: restart a headless vibe from its page, the page comes back by itself, and the out-of-date profile question appears on it.
- **Terminal sessions:** a vibe with a terminal session also serves a page; restarting it from the page would end the terminal's session. Offer Stop and Restart only for a headless vibe at first, and say why when it isn't (a tooltip on the disabled items).
- **Tests:** Go: the host refuses while a rebuild is under way, asks about mid-turn, gives the new vibe's log from an offset; jsdom: the menu opens and closes (Escape, outside click), the items are disabled with a reason where they can't apply, the dialog shows streamed output and a final state, the page keeps trying while the host is away and says when it's back; and the one real run above.

### Reporting an issue

- Menu items open a new GitHub issue in a new tab (`rel="noopener noreferrer"`): `https://github.com/fluffynuts/vibe/issues/new?template=bug_report.yml`, `…feature_request.yml`, `…other.yml`. They are vibe's own links, built by the page from constants, never from anything the agent said (the page deliberately never makes agent text clickable).
- **Prefill only what is harmless and useful:** the vibe version, the OS, the agent's name, the sbx version (`vibe --info` knows them): as query parameters the form fields take (`?template=bug_report.yml&version=…&os=…`). Never a path, a profile name, a token, a log line or anything from the conversation; the user pastes what they choose, after reading it.
- **Issue templates to write** (`.github/ISSUE_TEMPLATE/`): `bug_report.yml` (what happened, what was expected, steps, vibe/OS/sbx/agent versions, headless or terminal, what `vibe --info` says with a reminder to remove anything private, the log's last lines), `feature_request.yml` (the problem, what would help, alternatives, who it's for), `other.yml` (anything), and `config.yml` (blank issues allowed, a link to the readme's "troubleshooting" section if there is one). The same three are what the menu opens.
- **Tests:** the links are exactly the three, to the repository's own issues, with the template names that exist in `.github/ISSUE_TEMPLATE/` (a Go test lists that folder and checks each name the page uses is there: they can't drift); nothing private in the prefilled parameters (a test with a profile name, a path and a token in the environment: none appears).
- **Not decided:** the repository's name comes from a constant: a fork would want its own (read it from the build, or the `origin` remote?). Start with a constant.

## Order

1 (bold questions) first, alone: it is small and everything after benefits. 2's issue links and templates next (no host work). The restart half of the power menu last, and it is the one that wants discovery: do it as its own piece of work, with the real run, and a written finding before the code (as `docs/plan-model-effort.md` did).
