// The page's light or dark, as the user chose it in the power menu and the browser keeps. Loaded in the head, ahead of the
// page being drawn, so that it is drawn in the choice from the start and doesn't flash the system's first. The system's theme
// (the default) is no choice at all: no attribute on the page, and the stylesheet's own rules for a dark or a light system.
'use strict'
;(function () {
  const KEY = 'vibe-companion-theme'
  const CHOICES = ['dark', 'light']

  function kept() {
    try {
      const value = localStorage.getItem(KEY)
      return CHOICES.includes(value) ? value : ''
    } catch (e) {
      return ''
    }
  }

  function apply(choice) {
    if (CHOICES.includes(choice)) document.documentElement.setAttribute('data-theme', choice)
    else document.documentElement.removeAttribute('data-theme')
  }

  window.vibeTheme = {
    // current is 'dark' or 'light' when chosen, else 'system'.
    current: () => document.documentElement.getAttribute('data-theme') || 'system',
    // choose applies and keeps a choice; anything but 'dark' or 'light' is the system's.
    choose(choice) {
      apply(choice)
      try {
        if (CHOICES.includes(choice)) localStorage.setItem(KEY, choice)
        else localStorage.removeItem(KEY)
      } catch (e) { /* a private window: the choice holds for this page only */ }
    },
  }
  apply(kept())
})()
