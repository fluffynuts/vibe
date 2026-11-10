# The `playwright` feature: browsers installed at build time, headless Chromium and Firefox, owned by the agent

- **Status:** accepted, built 2026-10-28/29 (`feat/playwright-feature`, merged).
- **Decided:** 2026-10-28, with the user.
- **Came from:** `docs/plan-playwright.md`, now deleted: `git show 0555742:docs/plan-playwright.md`.

## Context

An agent (and a person) needing a browser found none and couldn't download one, more than once. A sandbox's home is rebuilt, so a browser
installed by hand is gone with it.

## Decision

`library/playwright/`: an install script run as root at build time, a kit fragment, and the agent's instructions. The feature's description
says it adds a few minutes to building the sandbox.

- `npm install -g playwright@latest` (a project that pins a version downloads its own revision anyway).
- **Headless Chromium** (`chromium-headless-shell`) and **Firefox**, in `/opt/ms-playwright`, which `PLAYWRIGHT_BROWSERS_PATH` points every
  Playwright at. The full Chromium and WebKit are not installed; the instructions say `npx playwright install` adds them, that the full
  Chromium is a large download (about 190 MB), and to prefer the installed headless browsers.
- **The folder belongs to the agent's user** (uid 1000), so a project's own revision can be added. Where that can't be done (no such user, or
  not root), the folders are made writable by all and a browser's own files stay as Playwright made them.
- **System libraries** (`playwright install-deps`, from apt) are fetched **while the browsers are**, since apt is the slowest part. The browsers'
  install is told not to check for libraries at its end (`PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS`), or every log would warn they are missing.
- **A failed step is a warning** in `/tmp/sbx-setup.log`, not a failed sandbox. A last step launches both browsers headless **as the agent's
  user** and logs their versions; a failed launch logs the first 15 lines of what the browser said.
- The kit lets the sandbox reach `registry.npmjs.org`, `cdn.playwright.dev`, `playwright.download.prss.microsoft.com`, the three
  `*.azureedge.net` hosts older versions download from, and Ubuntu's apt hosts (`ports` too, for arm64).

## What the real builds showed

- **The launch check ran as root with the agent's environment, and Firefox refused** ("Running Nightly as root in a regular user's session is
  not supported"), so the check said the browsers wouldn't launch when they did. An earlier trial through `sudo` never met the condition,
  because `sudo` resets the environment (use `sudo -E` to reproduce a build). Hence the check runs as the agent, and logs 15 lines on failure.
- **The libraries' check at the end of the browsers' install warned in every log**, hence the skip above.

## Measured (2026-10-28, Playwright 1.64.1, x86_64)

| | download | on disk |
|---|---|---|
| Chromium, full | 191.1 MB | 403 MB |
| Chromium, headless shell | 120.0 MB | 273 MB |
| Firefox | 111.0 MB | 310 MB |
| ffmpeg | 2.2 MB | 6 MB |
| system libraries (37 packages) | 33 MB | |

Installed (headless shell + Firefox): 233 MB download, 588 MB on disk. Times on one connection: both browsers one after the other 33.0 s, at
once 34.6 s (no gain), headless shell + Firefox in one command 18.9 s, **apt 87.3 s**: hence apt alongside the browsers, not the browsers
alongside each other. Real builds logged `installed Playwright 1.64.1: launched headless: chromium 156.0.8078.4, firefox 157.0`; both launch as
the agent; a project pinning 1.48.2 (this repo's Firefox tests) added its Firefox beside the latest and passed.

## Consequences

- The tests (`src/vibe/playwright_test.go`) run the script with stand-ins for `npm`, `playwright`, `node`, `su`, `id` and `chown`: order of
  calls, that libraries and browsers are fetched together (each stand-in waits, boundedly, for the other, so it is the meeting that is
  tested, not speed), each failure, ownership and permissions in both cases, and that the check runs as the agent. The real run is by hand.
- Since this was written, install steps that run npm as root use `npm_config_cache=/root/.cache/npm`, and a guard test requires it.
- **Accepted risk:** "latest" means a release that renames a browser target would show as a warning and a sandbox without browsers.

## Not verified

- An arm64 machine (Playwright picks its own builds and the kit allows `ports.ubuntu.com`, but it hasn't been run there); a sandbox for an agent
  other than Claude (the build step is the same); how long a whole build takes (the parts are measured, the whole never timed). No scheduled CI
  job runs the real script, which would catch an upstream rename early.
