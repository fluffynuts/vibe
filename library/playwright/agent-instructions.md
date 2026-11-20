## Playwright

Playwright for **Node** is installed globally (`playwright`, `npx playwright …`), with **headless Chromium** and **Firefox**
already downloaded, so a browser launches with no download. They are in `/opt/ms-playwright` (`PLAYWRIGHT_BROWSERS_PATH`
says so). There is no display: run headless, as Playwright does by default.

- **From a script**, `require('playwright')` works anywhere: Playwright is in `/usr/lib/node_modules`, which `NODE_PATH`
  points to (Node never looks in its global folder by itself). Then `const { chromium, firefox } = require('playwright')`
  and `await chromium.launch()`. An **ES module** (`.mjs`, or `"type": "module"`) ignores `NODE_PATH`: use
  `import { createRequire } from 'node:module'; const { chromium } = createRequire(import.meta.url)('playwright')`, or
  write the script as CommonJS (`.js` or `.cjs`).
- **In a project**, `npm i -D playwright` (or `@playwright/test`) and `npx playwright test`: the project's own copy is the
  one used. At the latest version the browsers above are used as they are. A project that pins another version needs
  its own revision of a browser: `npx playwright install firefox` downloads it into the same folder, and the network
  allows it.
- **From Python**: the Python bindings aren't installed, and a quick script is simplest in Node, above. If Python it
  must be, install the bindings at the **same version as the Node package** (`playwright --version`), so that they use
  the browsers already here: `pip install "playwright==$(playwright --version | sed 's/^Version //')"`. Any other
  version wants its own browsers and downloads them (a few hundred MB). If that exact version isn't on PyPI, the closest
  one with the same major and minor version usually uses the same browsers: `python -m playwright install --dry-run`
  says whether it would download anything. If the network refuses PyPI, ask the user.
- The full Chromium (a headed run, or `channel: 'chromium'`) and WebKit aren't installed: `npx playwright install
  chromium` or `webkit` adds them, and the full Chromium is a large download (about 190 MB). Prefer the installed
  headless browsers: they do what a headless test or screenshot needs. A headed run also needs a display, which there
  isn't.
- If `playwright` isn't there, its install failed: the reason is in `/tmp/sbx-setup.log`.
