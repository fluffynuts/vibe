# On Windows, what a vibe with no console starts opens no window; the folder is found where the sandbox has it

- **Status:** accepted, built 2026-11-26 on `fix/windows-hidden-processes`, a hotfix. Not yet tried on Windows by hand.
- **Decided:** with the user, 2026-11-26, after they saw the windows in a Windows 11 VM.
- **Came from:** `docs/plan-windows-hidden-processes.md` (never committed).

## Context

A Windows user saw terminal windows pop up all the time while vibe ran. In a VM, the user saw Windows Terminal windows open over
the page. A console program started by a process that has no console gets a new console of its own; the background vibe that
`--headless` leaves has none (it is started `DETACHED_PROCESS`), and it starts sbx, gh and its own repository helper every few
seconds while a page is open.

## Decision

- **Every program vibe starts goes through `internal/proc`** (`Command`, `CommandContext`, as os/exec's). On Windows, when vibe
  itself has no console (`GetConsoleWindow` is 0), the program is started `CREATE_NO_WINDOW`, its window hidden. A vibe with a
  console shares it with what it starts, which shows nothing new, and some of which need it (`sbx run`, ssh, dism, the
  installer), so nothing changes there. This rule, rather than sorting about forty calls into hidden and interactive ones: there is
  no list to get wrong, and an interactive program only ever runs from a vibe with a console.
- **Flags are added, not replaced:** the places that set how a program starts on Windows (`ignoreTerminalInterrupt`'s process
  group, `detach`'s `DETACHED_PROCESS`) add theirs with `proc.AddCreationFlags`, keeping the hiding.
- **Held mechanically:** `TestProgramsAreStartedThroughProc` fails on `exec.Command` or `exec.CommandContext` anywhere but
  `internal/proc` (tests aside). Windows-only unit tests pin the flags.

## Not verified

- On Windows, by hand: that the headless vibe's page no longer opens windows, and that a vibe in a terminal still runs `sbx run`
  and signs in as before. CI runs the Windows unit tests.

## The folder's path inside the sandbox (found in the same VM, fixed on the same branch)

**Context:** the page said "asking claude for its --help: … cd: C:\Users\Quickemu\code\yafs: invalid argument". vibe took the
sandbox's folder to have the same path inside as on the host, which holds on Linux and macOS; on Windows sbx mounts
`C:\Users\...` as `/c/Users/...`. Every conversation turn (`cd <folder> && claude …`), the model list, git run in the sandbox
(a new branch, the pull request list) and the push check were given the Windows path. The user wanted both fixed together: hiding
the windows alone left a Windows user unable to work.

- **`sbxrun.FolderInSandbox`**: the folder itself off Windows; on Windows, the first of the likely spellings
  (`sbxrun.WindowsMountCandidates`: `C:/Users/...`, `/c/Users/...`, `/mnt/c/...`, …, the memory store's list, now shared) that is a
  folder in the sandbox, found with **one** `sbx exec` and kept for the sandbox (its mounts are fixed when it is made). One that is
  in none is an error naming where it looked, not a guess.
- Used for the four commands run in the sandbox on its folder; what reads the folder on the host (the Review tab, the git status)
  keeps the host path.
- Not tried on Windows: tested here with a stand-in sbx under a pretend `/`.
