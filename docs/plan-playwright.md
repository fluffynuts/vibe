# The `playwright` feature

Built 2026-10-28/29 on `feat/playwright-feature`. Raised because an agent (and a person) needing a browser found none, and
couldn't download one, more than once: a sandbox's home is rebuilt, so a browser installed by hand is gone with it.

## What it does

`library/playwright/`: an install script run as root at build time, a kit fragment, and the agent's instructions. The
feature's description says it adds time to building the sandbox (a few minutes).

- `npm install -g playwright@latest` (the latest: a project that pins a version downloads its own revision anyway).
- **Headless Chromium** (Playwright's `chromium-headless-shell`) and **Firefox**, in `/opt/ms-playwright`, which
  `PLAYWRIGHT_BROWSERS_PATH` points every Playwright at. The full Chromium (headed runs, `channel: 'chromium'`) and
  WebKit are not installed; the instructions say `npx playwright install` adds them, that the full Chromium is a large
  download (about 190 MB), and to prefer the installed headless browsers.
- **The folder belongs to the agent's user** (uid 1000), so a project's own revision can be added to it. Where it can't be
  given (no such user, or not root), the *folders* are made writable by all, and a browser's own files are left as
  Playwright made them: readable and runnable by all, writable by their owner only.
- The system libraries (`playwright install-deps`, from apt), **fetched while the browsers are**: apt is the slowest
  part. The browsers' install is told not to check for the libraries at its end (`PLAYWRIGHT_SKIP_VALIDATE_HOST_REQUIREMENTS`),
  which are being fetched at that moment, or every build log would warn the host is missing them.
- A failed step is a warning in `/tmp/sbx-setup.log`, not a failed sandbox. A last step launches both browsers headless
  **as the agent's user** (through `su` when the script is root) and logs their versions; a launch that fails logs the
  first 15 lines of what the browser said, which name why.
- The kit lets the sandbox reach `registry.npmjs.org`; `cdn.playwright.dev` (the only host the current Chromium is
  offered from) and `playwright.download.prss.microsoft.com` (Firefox's fallback); `playwright.azureedge.net`,
  `playwright-akamai.azureedge.net` and `playwright-verizon.azureedge.net` (where older versions download from, which
  a project pinning one needs); and Ubuntu's apt hosts (`archive`, `security`, and `ports` for arm64).

## What the real builds showed

- **The launch check ran as root with the agent's environment, and Firefox refused.** A sandbox build runs the script as
  root, but with the agent's `HOME` and `XDG_RUNTIME_DIR`; Firefox won't start so (`Running Nightly as root in a regular
  user's session is not supported`), though it starts for the agent. The check said the browsers wouldn't launch when
  they did. It took a second build to see why: the first log kept one line of the error, and an earlier trial as root had
  gone through `sudo`, which resets the environment, so it never met the condition. Hence the check runs as the agent
  (what the browsers are for), and its failure logs 15 lines. To reproduce a build's condition, use `sudo -E`.
- **The libraries' check at the end of the browsers' install warned in every log**, as above.

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
- Run for real, on x86_64: the script against the real downloads into scratch folders (as root with a kept environment,
  as a build runs it), and the feature in real sandbox builds: the setup log says `installed Playwright 1.64.1: launched
  headless: chromium 156.0.8078.4, firefox 157.0`; both launch as the agent and take a screenshot; a project pinning
  1.48.2 (this repo's Firefox tests) added its Firefox beside the latest as the agent and passed; the instructions arrive
  in the kit's context file.

## The tests

`src/vibe/playwright_test.go` runs the script as it is with stand-ins for `npm`, `playwright`, `node`, `su`, `id` and
`chown` on a PATH of its own, so no real one is found: what it asks for and in what order, that the libraries and the
browsers are fetched together (each stand-in waits, boundedly, for the other to have started, so it is the two meeting
that says so, not how fast the machine is), what it does when each step fails, the ownership and the permissions in both
cases, and that the check runs as the agent when the script is root and never calls `su` when it isn't. The real run is
by hand, as above.

## Not tried

- An arm64 machine. Playwright picks its own builds and the kit allows `ports.ubuntu.com`, but it hasn't been run there.
- A sandbox for an agent other than Claude (the build step is the same).
- How long a full build takes: the parts are measured (above), the whole never timed.
- Future versions of Playwright: "latest" means a release that renames a browser target would show as a warning and a
  sandbox without browsers. A scheduled CI job running the real script would catch it early; none exists.
