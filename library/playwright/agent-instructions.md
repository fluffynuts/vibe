## Playwright

Playwright is installed (`playwright`, `npx playwright …`), with **headless Chromium** and **Firefox** already
downloaded, so a browser launches with no download: `playwright.chromium.launch()`, `playwright.firefox.launch()`,
`--project=chromium`, `--project=firefox`. They are in `/opt/ms-playwright` (`PLAYWRIGHT_BROWSERS_PATH` says so).
There is no display: run headless, as Playwright does by default.

- In a project, `npm i -D playwright` (or `@playwright/test`) and `npx playwright test`. At the latest version the
  browsers above are used as they are. A project that pins another version needs its own revision of a browser:
  `npx playwright install firefox` downloads it into the same folder, and the network allows it.
- The full Chromium (a headed run, or `channel: 'chromium'`) and WebKit aren't installed: `npx playwright install
  chromium` or `webkit` adds them. A headed run also needs a display, which there isn't.
- If `playwright` isn't there, its install failed: the reason is in `/tmp/sbx-setup.log`.
