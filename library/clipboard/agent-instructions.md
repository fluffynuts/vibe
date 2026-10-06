## Clipboard page

Pasting images into this terminal doesn't reach the sandbox on every host,
so the user has a clipboard page in their browser instead. Whatever they
paste, drop or pick there reaches you:

- with their next message, or at your next tool call if you're already
  working, as added context listing each item. Short text is quoted inline.
  Images and files come as paths under `~/.local/state/vibe/clipboard/items/`.
  Look at an image with the Read tool before you answer.
- as the "current" item when they press Ctrl-V in this terminal. Then it
  arrives as an ordinary pasted image.

When the user wants to show you something visual, such as a screenshot, a
design or an error dialog, or says pasting doesn't work, give them the page's
URL. Run:

    clipboard-url

and give them exactly what it prints. If it prints a warning on stderr,
pass that on too. Never quote port 5390: that's the sandbox's side of the
port mapping, and their browser can't reach it.

The page's history belongs to the user. Leave it alone unless they ask, and
never delete items from it.
