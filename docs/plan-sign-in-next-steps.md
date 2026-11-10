# Signing in: what is left

What was built is in `docs/adr/2026-10-29-signing-in.md`. Nothing here is built.

## An API key from the page

- A field on the Sign-in card that runs `sbx secret set anthropic` with the key on **stdin** (never on a command line, in a log, or kept),
  beside the subscription flow. `sbx secret set` also takes `-t` (visible in shell history), `--ref` (1Password/AWS) and `--command`;
  `--oauth` is openai only (anthropic only with `--cloud`), so a subscription can't go through `sbx secret`. The "Do it yourself" steps already
  show the command.
- Cursor, and other agents, the same way once their sign-in can be read; build alongside Claude's with the same structure so the next agent can
  generalise (there is no Cursor plan yet).

## The keyring stall

- `sbx secret ls` and `sbx ls` stalled on a host whose KWallet waited for a password; sbx uses the system keyring. **Unverified** suggestion:
  point `DBUS_SESSION_BUS_ADDRESS` at a bus that doesn't exist, `sbx daemon restart`, and sbx says "No keychain detected". If it works it must be
  the user's choice, said plainly (a file in place of the keyring is a weaker store). Same family as the shelved keyring-hang notes.
- Open: does sbx honour it, and what does it print?

## Left from the signed-out start

- Letting the terminal flow finish for the page: signed out on a terminal, `sbx exec`'s own flow starts the sandbox once confirmed; the page has no
  terminal, so it cannot.
- A language-neutral signal for `sbx ls` failing (`signin.Docker` matches "not signed in").
- What signs sbx out other than `sbx logout` (expiry?), and whether the few seconds `sbx login` spends after "You are signed in" matter to the
  card's button.
- Offering the agent's sign-in right after the first sandbox is created (on the terminal), or an API key at setup.
