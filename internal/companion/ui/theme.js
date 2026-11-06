// The page's light or dark, as the user chose it in the power menu. Loaded in the head, ahead of the page being drawn, so that it
// is drawn in the choice from the start and doesn't flash the system's first. The system's theme (the default) is no choice at
// all: no attribute on the page, and the stylesheet's own rules for a dark or a light system.
//
// The choice is kept in a cookie, not in localStorage: a cookie is shared by every port of a host, so one choice holds for each
// sandbox's page (each is on a port of its own) and the Profiles page, where localStorage is a page's own. The cookie is read
// again when a page comes back into view and on its poll (sync), so a page that is open follows a choice made in another.
'use strict'
;(function () {
  const NAME = 'vibe-theme'
  const CHOICES = ['dark', 'light']
  const A_YEAR = 365 * 24 * 60 * 60

  function kept() {
    try {
      const found = document.cookie.split('; ').find((c) => c.startsWith(NAME + '='))
      const value = found ? found.slice(NAME.length + 1) : ''
      return CHOICES.includes(value) ? value : ''
    } catch (e) {
      return ''
    }
  }

  function apply(choice) {
    if (CHOICES.includes(choice)) document.documentElement.setAttribute('data-theme', choice)
    else document.documentElement.removeAttribute('data-theme')
  }

  // What the cookie said when it was last looked at: a page whose browser keeps no cookies holds its choice for itself, and is
  // not put back to the system's by a cookie that never changes.
  let seen = kept()

  window.vibeTheme = {
    // current is 'dark' or 'light' when chosen, else 'system'.
    current: () => document.documentElement.getAttribute('data-theme') || 'system',
    // choose applies and keeps a choice; anything but 'dark' or 'light' is the system's. Host-only (no Domain), whatever the path.
    choose(choice) {
      apply(choice)
      try {
        document.cookie = CHOICES.includes(choice)
          ? NAME + '=' + choice + '; Path=/; Max-Age=' + A_YEAR + '; SameSite=Lax'
          : NAME + '=; Path=/; Max-Age=0; SameSite=Lax'
      } catch (e) { /* no cookies: the choice holds for this page only */ }
      seen = kept()
    },
    // sync follows a choice made elsewhere (another tab), and says whether the page changed.
    sync() {
      const now = kept()
      if (now === seen) return false
      seen = now
      apply(now)
      return true
    },
  }
  apply(seen)
})()
