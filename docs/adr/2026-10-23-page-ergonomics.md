# Page ergonomics: questions that stand out, a power menu, and a way to report an issue

- **Status:** accepted, built: the bold questions 2026-10-24 (`feat/bold-questions`), the power menu and issue links 2026-10-28 (`feat/power-menu`). Parallel
  `--update-sandboxes`, the third part of the plan, is not built: `docs/plan-parallel-sandbox-update.md`.
- **Decided:** 2026-10-23/24, with the user ("small things, bundled: each a day's work, and none needs its own plan to decide").
- **Came from:** `docs/plan-quality-of-life.md`, now deleted: `git show 4aaf4f5:docs/plan-quality-of-life.md`.

## Decision

### 1. A question asked on the command line stands out

The whole of a question is drawn in the terminal's **bright yellow (SGR 93)**, the shade the WARNINGs use, and nothing else of the prompt: not the `vibe: ` prefix,
not the `[Y/n]` hint, not a picker's list, not the answer typed. A long or multi-line question is all of it in colour, as one call passes it (decided with the user,
2026-10-24). It was plain bold first, and didn't stand out enough in play-testing. `askText` (`src/vibe/gittoken.go`) says it, on top of the existing `paint` (off
when `NO_COLOR` is set or stderr isn't a terminal; `prompt.EnableVT` already turns escapes on for Windows), and `noteAs` shows the styled line on the terminal while
the page's log and held notes get the plain words, so the text is the same wherever it is read.

**Mechanical, so it stays so:** a test reads the Go source and fails for any function that opens the terminal to ask (through `ttyReader`, `ttyRW`, `ReadMasked`, or
`prompt.Open` directly) and doesn't say its question with `askText`. A search for `Fprint*(os.Stderr, "vibe: …` would have missed a picker's question.
A colour-blind reader, or a theme where yellow can't be told from the background, is the case to watch; play-testing, not a test, will say.

### 2. A power menu on the page

A **power button** (the power switch's symbol) in the page's top bar replaces the "connected" indicator and opens a menu: **Stop this session**, **Restart**, and, after a
divider, **Report a bug**, **Request a feature**, **Something else**. The aim is that the user no longer has to go to a console to `vibe --restart`.

- **It says what the indicator said, in less room.** Connected is a lit button with a soft glow; not connected is flat and dim; connecting (before the first answer, and
  during the one failure `reached` tolerates) is dim and pulses slowly (steady under `prefers-reduced-motion`). **Colour is not the only signal** (red and green are
  the pair the commonest colour-blindness can't tell apart), so they differ in brightness and glow as well, which the tests check. The indicator's text moves to the
  button's `title` and `aria-label`, a visually hidden live region that tells a screen reader when the connection changes, and the menu's first line. The glow and the
  dull red each meet 3:1 against the bar in both themes. When the connection is down the menu still opens, because the issue links work with no vibe; Stop and Restart
  are shown disabled, with the reason in the menu itself, not in a tooltip a touch screen would miss.
- **It is a power symbol, so it must not be pressed by accident:** it opens a menu and never acts on click; Stop asks first when the agent is mid-turn, in a dialog;
  Restart says what it will do.
- **Each action is the CLI's own command,** run apart from the vibe serving the page (`powerDesk`, `src/vibe/power.go`): `vibe --stop --force`,
  `vibe --restart --force --no-auto-update`, `vibe --headless --no-auto-update`, each `--name <sandbox> -- <folder>`, from the running vibe's own executable, detached, writing
  to `~/.vibe/logs/<name>.power.log` with its PID and start time beside it (so the page can tell when it ends, and a PID reused by something else is not taken for it after 30
  minutes). The calls are `GET /api/power?sandbox=` (what each action can do, and what it would interrupt), `POST /api/power` and `GET /api/power/log?sandbox=&from=` (the log
  from a byte offset, whether the action still runs, and the server's `boot`). A sandbox name is validated before it reaches a file name; a POST that would interrupt
  something is refused unless it is `confirmed` and sends back every warning it would now give, so what was shown is what is confirmed; while an action runs on a sandbox, every
  action on it is refused; and a rebuild under way refuses everything, with the reason. **One backend keyed by sandbox name:** the top-bar items use this vibe's own name, the
  **Profiles tab** calls it for any listed sandbox (**Start**, **Stop**, **Restart**), and the confirmation names the sandbox and its folder, every time, since the page acted
  on is not the one looked at.
- **Restart and Start are headless only:** Start can't give a terminal session, so it starts a headless vibe and shows its page link; a vibe with a terminal session can be
  stopped (with a warning) but not restarted from the page, and says why. A restart of the page's own sandbox is over when the log is answered under a new `boot` and the
  helper has finished, which is when the new vibe has said its page is up and its sandbox started; the page then says so and refreshes itself two seconds later. Past about two
  minutes without that, the box says to reload. The new vibe is started with `VIBE_COMPANION_NO_OPEN=1` so it doesn't open a second tab.

### 3. A way to report an issue

The three menu items open a new GitHub issue in a new tab (`rel="noopener noreferrer"`) from `.github/ISSUE_TEMPLATE/` (`bug_report.yml`, `feature_request.yml`, `other.yml`). They
are vibe's own links, built by the page from constants, never from anything the agent said. Only what is harmless is prefilled (vibe's version, the OS, the agent's name, the sbx
version), never a path, a profile name, a token, a log line or anything from the conversation: the user pastes what they choose. A Go test checks that each template name the
page uses exists in the folder, so they can't drift.

## Consequences

- A prompt can't scroll past unseen among vibe's notes, and the stop/restart/issue actions are on the page.
- The page acts on other vibes' processes only through the CLI, which already knew how to ask, save memories, and refuse when something is running.
- The power menu is also where an **Appearance…** item would go (`docs/plan-accessibility.md`); this work only had to leave no hard-coded colour or fixed `px` font size, label
  every control, and keep the power button's states in CSS variables.

## Not built, on purpose

- **The out-of-date questions on the page** (the "stage 4"). A vibe started from the page has no terminal, so its out-of-date countdown would rebuild the sandbox at once with no
  way to skip. Instead the page's Start and Restart pass `--no-auto-update` and say, in the confirmation, that an out-of-date sandbox isn't updated by this (run `vibe <folder>` in a
  terminal for that). Asking those questions on the page needs the new vibe to serve its page before it asks, which restructures start-up; build it when it is wanted and a real sbx
  can prove it.
- The repository's name in the issue links comes from a constant; a fork would want its own (read from the build, or the `origin` remote?).

## Not verified

- The power menu's actions were not tried with a real sbx when they were built (the page's tests use stand-ins).
