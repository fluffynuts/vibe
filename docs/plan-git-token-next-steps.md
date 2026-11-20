# GitHub tokens: what is left after `--batch-git-token` (planned, not built; small)

What is built is `docs/adr/2026-11-12-batch-git-token.md`. Keeping tokens in the OS credential store is its own plan:
`docs/plan-token-storage.md`.

## Naming the sandboxes for a script

`vibe --batch-git-token --sandboxes a,b,c` (with `--git-token-env VAR` for the token), so the batch runs with no terminal. Unknown names are
refused, saying which, before anything is given. Until then the batch needs a terminal to tick the sandboxes on.

## A check from inside the sandbox

The check today is on the host, with GitHub, for each sandbox's repository: it says the token can push, not that the sandbox has it. An
agent once checked with `gh api repos/<owner>/<repo>` from inside the sandbox, which tests the token as the sandbox holds it. Whether vibe
can do that cheaply (one `sbx exec`, using whatever the sandbox's `gh` or `git` takes the secret from) is not known: to be tried before it is
planned further. It must not wake a stopped sandbox.

## Tokens near expiry, on the Admin tab

The page already shows a sandbox's own token and its expiry. Across sandboxes, it could say which are near expiry, from vibe's records,
with no call to GitHub, and that `vibe --batch-git-token` renews several at once.
