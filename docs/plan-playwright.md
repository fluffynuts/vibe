# The `playwright` feature

Built 2026-10-28 on `feat/playwright-feature`. Raised because an agent (and a person) needing a browser found none, and
couldn't download one, more than once: a sandbox's home is rebuilt, so a browser installed by hand is gone with it.

## What it does

`library/playwright/`: an install script run as root at build time, a kit fragment, and the agent's instructions.

- `npm install -g playwright@latest` (the latest: a project that pins a version downloads its own revision anyway).
- **Headless Chromium** (Playwright's `chromium-headless-shell`) and **Firefox**, in `/opt/ms-playwright`, which
  `PLAYWRIGHT_BROWSERS_PATH` points every Playwright at. The agent user owns the folder, so a project's own revision can
  be added to it. The full Chromium (headed runs, `channel: 'chromium'`) and WebKit are not installed.
- The system libraries (`playwright install-deps`, from apt), **fetched while the browsers are**: apt is the slowest part.
- A failed step is a warning in `/tmp/sbx-setup.log`, not a failed sandbox. A last step launches both browsers headless
  and logs their versions; a launch that fails says why.
- The kit lets the sandbox reach `registry.npmjs.org`, `cdn.playwright.dev` (the only host the current Chromium is
  offered from), `playwright.download.prss.microsoft.com` (Firefox's fallback), and Ubuntu's apt hosts.

## What was measured (2026-10-28, Playwright 1.64.1)

| | download | on disk |
|---|---|---|
| Chromium, full (`chrome-linux64`) | 191.1 MB | 403 MB |
| Chromium, headless shell | 120.0 MB | 273 MB |
| Firefox | 111.0 MB | 310 MB |
| ffmpeg | 2.2 MB | 6 MB |
| system libraries (37 packages, from a sandbox that had some) | 33 MB | |

- Headless shell + Firefox (what is installed): 233 MB to download, 588 MB on disk. With the full Chromium as well:
  424 MB, 990 MB.
- Time on one connection: `playwright install chromium firefox` one after the other 33.0 s; both at once 34.6 s, so no
  gain (Firefox is 5 s of it); headless shell + Firefox in one command 18.9 s; **apt 87.3 s**. Hence apt alongside the
  browsers, and not the browsers alongside each other. Two `playwright install` runs into one folder don't lock each other.
- Run for real (the script, as root, against the real downloads, into scratch folders): both launched headless, as root
  and as the agent; a project pinning 1.48.2 added its Firefox beside the latest as the agent.

## Not tried

- A from-scratch build of a sandbox with the feature (the apt step ran here with most libraries already present).
- An arm64 machine.
