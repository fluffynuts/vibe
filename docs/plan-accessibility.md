# An accessibility sweep, and options for how the page looks (planned, not built)

Raised 2026-10-23, as a note on the power menu (`docs/plan-quality-of-life.md`): a sweep of the page for accessibility, and options (more contrast, less animation, bigger text), possibly with a lighter theme. It wants a lot of back and forth to get right, so it is its own branch, **after** the power menu's.

## What there is (read from the code, not run)

- **There already is a light theme.** The page's colours are CSS variables: `:root` holds the light set, and `@media (prefers-color-scheme: dark)` (`app.css`, near the top, and again for the dialogs' glass) overrides them; `<meta name="color-scheme" content="light dark">`. It looks dark for someone whose system is set dark. What is missing is a *choice*: no way to pick light or dark but by changing the operating system's setting.
- 71 `aria-*` attributes in `index.html`, 14 `:focus-visible` rules, five `prefers-reduced-motion` rules (the typing dots, a few more). No `prefers-contrast` and no `forced-colors` (Windows high contrast) rule anywhere.
- Sizes are fixed: 49 `px` font sizes and five `em`s, none in `rem`. Browser zoom works; nothing scales text alone.
- The page's CSP is `script-src 'self'` (`server.go`): no inline script, so anything that must run before first paint (to apply a saved theme without a flash) has to be a file the page loads from its own origin, in the head.
- Preferences about how the page looks are the viewer's, not the profile's: the page keeps those in the browser (`localStorage`, like "don't show me this again" in the model/effort bubble).

## 1. The sweep (find, then fix; each finding with a test)

- **Keyboard:** every control reachable and operable from the keyboard, in a sensible order; nothing a trap (dialogs are native `<dialog>`, which holds focus and gives Escape: check each one handles Escape deliberately, as the git author dialog deliberately doesn't); a visible focus on everything; the comment arrows and shortcuts documented where they are used.
- **Names and announcements:** every control has an accessible name (icon-only buttons: the copy button, the info button, the power button, the arrows); status changes that matter are announced (`role="status"`/`alert` and live regions: the connection, the agent finishing, a toast, an error); the Conversation log's own live region doesn't announce every tool line (the groups help).
- **Colour is never the only signal:** the git status in the bar (`+3 ~2 ?1`) is coloured *and* signed, but the diff's red and green, the connection state, the review's open/resolved marks, the tick colours of the prompts and the "bad" text in forms need checking against that rule; add a sign, a shape or a word where there is only a colour.
- **Contrast:** each text and each UI component against its background meets WCAG AA (4.5:1 text, 3:1 components and large text), in both themes, with the agent's tint (`themeColor`) applied, which has to be clamped so a chosen colour can't take the page under the minimum.
- **Reflow and size:** usable at 200% zoom and at 320px wide (the container query at 920px for the prompt bar exists: check the rest), no horizontal scrolling of the page, targets at least 24px.
- **Motion:** every animation and smooth scroll is covered by the reduced-motion rule, not only five of them.

**How it is tested without a tool from outside the repo** (Playwright's Firefox is installed; `axe-core` is not, and is a download needing the user): a pure-JS test reads the CSS variables of each theme and mode, and computes the WCAG ratio for each foreground/background pair that the page actually uses (listed once, in the test, so adding a pair is a line); a Firefox test emulates `prefers-reduced-motion`, `prefers-color-scheme`, `forced-colors` and `prefers-contrast` where Playwright can (check which Firefox honours) and asserts the computed styles; a keyboard-walk test per tab presses Tab from the top and checks every control with a role is reached once, with a visible focus, and nothing traps; jsdom tests for names and live regions. If `axe-core` turns out to be worth having, ask for it (it's from npm: the user's to allow).

## 2. Options (an "Appearance…" item in the power menu, a dialog)

| Option | Values | How |
|---|---|---|
| Theme | System (default), Light, Dark | `data-theme` on `<html>`; the dark variables move from the media query into a `[data-theme=dark]` rule as well (the media query stays for *System*), so one block serves both |
| Contrast | System (default; follows `prefers-contrast: more`), Normal, High | `data-contrast`: a variable set with stronger text, lines and focus rings, and the tint ignored |
| Motion | System (default), Reduced | `data-motion=reduced` turns animations and smooth scroll off |
| Text size | 90%, 100%, 115%, 130%, 150% | the root font size, with every `px` font size (and the paddings that should follow) moved to `rem` — a large, mechanical diff, done in one commit by itself |
| Colour-blind-safe marks | off/on | adds signs and shapes to the red/green places the sweep finds, if the sweep shows those can't always be there |

- **Kept per browser** (`localStorage`), applied by a small external script loaded first in the head (CSP forbids inline) so there is no flash of the wrong theme; with a clear "reset to the system's" button.
- **Windows forced colours** (`forced-colors: active`) is its own check: the page should be legible there with the browser's palette, using system colours for borders and focus: a short block of rules, tested by emulation if Firefox allows, by hand otherwise.
- **Not in `user-preferences.yaml`**: that is per profile, on the host; how a person likes to see the page follows the person and the browser.

## Branch and order

- A branch of its own, `feat/accessibility`, off master **after** the power menu branch has merged: it touches nearly every CSS rule (`px` to `rem`, the variable blocks), so it wants a different kind of review (screenshots in both themes at each size) and it would bury the power menu's diff.
- **One rule for the power menu's branch, so the sweep isn't made bigger:** no hard-coded colour or new fixed `px` font size; every new control labelled; the power button's states (glow, dull, connecting) are CSS variables, so a contrast mode can change them.
- Within the branch: the sweep's findings and fixes first (no options needed to fix a missing label or a contrast failure), then the theme choice (small, the variables are there), then contrast and motion, text size last (the biggest diff).

## Not decided

1. Which sizes, and whether text size scales only text or the whole page (the browser's zoom already does the second).
2. Whether the high-contrast set is one set or follows the theme (a light and a dark high-contrast).
3. Whether to write the tint clamp as a rule in `applyTheme` now (it is a fix, whatever the options do).
4. A skip link ("Skip to the conversation") for keyboard users, given the tab bar above the content.
