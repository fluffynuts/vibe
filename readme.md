# vibe

Create, start and attach to [Docker Sandboxes](https://github.com/docker/sbx-releases) for
contained agentic coding — driven by **profiles**, so different teams/repos can bring their
own tooling (a .NET + Elasticsearch profile, a Rails profile, whatever a repo needs) without
touching the tool itself. 

It runs on Linux, macOS and Windows — wherever `sbx` does.

Whilst the initial implementation is focused around using Claude within the container,
in theory, any agent supported by sbx can be used.

This software is in alpha state. It works reasonably well for me, but I make no guarantees.

## Requirements

Vibe uses Docker Sandboxes (sbx) under the hood. This immediately sets limitations on
supported installation targets:

1. Linux: just about any distro with kvm enabled
2. Windows: at least windows 11
3. OSX: requires Apple silicon

## Install

vibe needs [Docker Sandboxes (`sbx`)](https://github.com/docker/sbx-releases) installed. Once vibe is
installed, `vibe --install-sbx` installs (or upgrades) it for you, from sbx's latest stable
release (never an RC or nightly):

- **Linux:** it runs the install script inside the release, with only its AppArmor step under
  `sudo`, so sbx still lands in your own `~/.docker/sbx`, not root's.
- **macOS** (Apple Silicon, Sonoma or newer): it asks how. It can unpack the release into
  `~/.docker/sbx` (no admin rights needed). It can copy `Sbx.app` from the release's `.dmg` into
  `/Applications`, linking `sbx` into `~/.docker/sbx/bin`. Or, when Homebrew is installed, it can
  run `brew install --cask docker/tap/sbx`.
- **Windows** (x64): it asks whether to run the release's MSI just for you (no admin rights
  needed) or for every user (Windows asks for admin rights).

Either way it tells you if sbx's folder isn't on your `PATH` (on Windows, the MSI adds it, so
you open a new terminal), and prints the version it installed. `-f` takes the first option
without asking, and reinstalls when the latest release is already installed.

**Linux and macOS:**

```sh
curl -fsSL https://raw.githubusercontent.com/fluffynuts/vibe/master/install.sh | sh
```

**Windows** (PowerShell):

NB: docker sandboxes do not support windows 10. You will need at windows 11.

```powershell
irm https://raw.githubusercontent.com/fluffynuts/vibe/master/install.ps1 | iex
```

Either one downloads the latest release for your machine, checks it against the release's
checksums, and runs `vibe --install` from it. That puts the `vibe` binary in `~/.local/bin` and
its configuration in `~/.vibe`. On Windows it adds `~\.local\bin` to your user `PATH` if it isn't
there already (open a new terminal to pick it up); on Linux and macOS, where that depends on your
shell's profile, it tells you what to add. If `sbx` isn't on your `PATH`, it offers to install it for you
(with `-f`, or no terminal to ask on, it only tells you to run `vibe --install-sbx`).
On Windows, it also checks that the Windows features sbx needs, `HypervisorPlatform` and
`VirtualMachinePlatform`, are on, and enables them with `dism` if they aren't. `dism` needs admin
rights even to look, so unless vibe is already running as administrator, it asks first and Windows
asks for admin rights. If it had to enable them, **restart Windows** before using vibe. Once they're
found on, later installs don't check again.
On Linux, it checks that `/dev/kvm` exists (if not, the kvm kernel modules aren't loaded, or
virtualization is off in your BIOS/UEFI) and that you can read and write it. If you can't, it names
the group to add yourself to (usually `kvm`), or tells you to log out and back in if you've been
added since you logged in (or, without logging out, `newgrp kvm`, then vibe from the shell it starts).
The install only warns; after that, vibe checks again before anything that would start a sandbox
(creating, attaching, `--headless`, a rebuild, `--update-sandboxes`, Start or Rebuild on the page),
and refuses with the same advice, rather than leave sbx to fail part-way.
`install.sh` needs `curl` or `wget`, and `unzip` (or `python3`).

Run the same command again to upgrade, or, once vibe is installed:

```sh
vibe --upgrade
```

To pass options to the `vibe --install` they run, such as an `--update-strategy` (see
[Upgrading](#upgrading)):

```sh
curl -fsSL https://raw.githubusercontent.com/fluffynuts/vibe/master/install.sh | sh -s -- --update-strategy merge,keep
```

```powershell
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/fluffynuts/vibe/master/install.ps1))) --update-strategy 'merge,keep'
```

## Download

Every push to master publishes a [release](https://github.com/fluffynuts/vibe/releases/latest)
with a zip per platform, each for x86-64 and ARM: `vibe-linux-amd64.zip`,
`vibe-macos-arm64.zip`, `vibe-windows-amd64.zip` and so on, plus a `SHA256SUMS`. The names don't
change between releases, so `https://github.com/fluffynuts/vibe/releases/latest/download/<name>`
always gets the newest. To install by hand, unzip it anywhere and run `vibe --install` from the
folder it makes. The binary reads its bundle from beside itself, so keep the folder together.
macOS may block the unsigned binary the first time: `xattr -dr com.apple.quarantine <folder>`.

### Upgrading

`vibe --upgrade` (`-U`) checks GitHub for newer releases of vibe and of Docker SBX, then lists
what it found with everything selected, for you to deselect what you'd rather leave; enter starts the
upgrades straight away, with no confirmation step. When there's
nothing newer, it says "vibe and sbx are up to date". Deselecting everything stops with "nothing
selected to update" and a non-zero exit. `-f`, or running with no terminal, upgrades everything
it found without asking.

- **vibe:** it downloads this machine's zip into a temporary folder, checks it against the
  release's `SHA256SUMS`, unpacks it into another, and runs `vibe --install` from there. That
  replaces the installed `vibe`, even while another vibe session is running it. `-f` and
  `--update-strategy` are passed on to that `--install`.
- **Docker SBX:** it's upgraded to its latest stable release, the same way `--install-sbx` installed
  it (on macOS and Windows, whichever way you picked). An sbx that `--install-sbx` didn't
  install, such as one from a `.deb`, is left for you to upgrade the way you installed it.

While a sandbox session runs, vibe makes the same checks in the background. When the session ends,
it lists anything newer it found and suggests `vibe --upgrade`. It says nothing when everything is up
to date, when a check fails, or when the checks haven't finished by the time you exit.

The first `vibe --install` (and vibe's first run, if you skip `--install`) also sets up
`~/.vibe/settings.yaml` for your machine. It asks how much memory each sandbox gets, in 4g steps
up to half your machine's memory, and how many CPUs, any number from 1 to one less than your machine
has, starting on half of them (see [CPUs](#cpus-a-sandbox-gets)). It asks which agent sandboxes run by default, from the agents `sbx run
--help` lists, and which library features a guided profile starts with selected. Each question
starts on the package's choice. Creating a new sandbox asks again which agent it should run,
starting on that default (or on the profile's own `agent`, when the profile exists already). It asks
this first, before any profile is made, because a guided profile only offers the features that work
with that agent; a re-init or re-create keeps the agent the sandbox was created with. With `-f`, or no terminal, it keeps the package's choices and only
lowers the memory, if it's more than your machine can give (and a `cpus` already set, if it's more
than your machine can spare). Every later `--install` makes the same checks on your `settings.yaml`,
and changes nothing else in it.

`vibe --setup` asks those questions again (default memory, CPUs, default agent, and the features a guided
profile starts with selected), starting on your current choices, and saves the answers in
`~/.vibe/settings.yaml`. It then offers to store a default GitHub token for new sandboxes to push with
(see [Letting the agent push](#letting-the-agent-push)). It needs a terminal, and
`~/.vibe/settings.yaml` to exist (so run `vibe --install` first).

`vibe --install`, run from a release's folder (which is all the install scripts and `--upgrade`
do), goes through every file the package ships
into `~/.vibe` (`config.yaml`, `settings.yaml`, `defaults/`, `profiles/`, `library/`) and keeps
your edits:

- **New in this release:** copied, and listed.
- **You never edited it:** updated to the new version without asking.
- **Only you changed it:** kept as it is.
- **You deleted it:** stays deleted.
- **No longer in the package:** removed if you never edited it. If you did, you're asked whether to
  keep yours or remove it. With nobody to ask it's kept, and listed at the end for you to delete if
  you don't need it.
- **A feature that is a base feature now** (like `clipboard-bridge`): an old copy of it still in
  `~/.vibe/library/` is never used any more. You're asked whether to remove it or move it to
  `~/.vibe/retired/library/`, and with nobody to ask it's moved.
- **Changed by both you and the release:** you're asked. If the two sets of changes merge
  cleanly (git's three-way merge, so git needs to be installed), you see your file beside the
  merged result and choose between keeping yours, using the merged version, or overwriting yours
  with the new one. That last choice first shows your file beside the new one, to confirm. If they
  don't merge, you see your file beside the new one and choose between keeping yours and
  overwriting it.

This works by keeping the package's version of every file from the last install in
`~/.vibe/package-files/`. That's what tells your edits apart from the release's, the way git uses
a merge base. A file you keep holds on to its old original, so a later upgrade can still merge
it. Files installed before `package-files` existed have no original, so the first upgrade can't
merge them and asks you to keep or overwrite each one that differs. Your own files in `~/.vibe`
are never touched: `instances/`, `memories/`, profiles vibe generated for you, and so on.

With `-f`, or with no terminal to ask on, a file changed on both sides is left as it is, and the
install ends with a warning and a non-zero exit. `--update-strategy` (`-u`) settles those files
without asking, and also runs unattended:

| Strategy | Merges what merges | Everything else |
|---|---|---|
| `merge,keep` (or `merge`) | yes | yours is kept, with the new version beside it as `<file>.updated` to merge by hand |
| `merge,update` | yes | overwritten with the new version |
| `update` | no | overwritten with the new version |
| `keep` | no | yours is kept, and no `.updated` files are written; run again with another strategy to take the changes later |

A `.updated` file is never used by vibe itself: it doesn't run as an install script or get
copied into a sandbox.

### Keeping sandboxes up to date

New vibe features, like the clipboard bridge or `vibe-review`, reach a sandbox when its profile is
composed again from the library and the sandbox is rebuilt from the profile. vibe does both for you,
when they're needed:

- **A profile** is out of date when composing it again, from the features it records, would give
  something different. Composing records a hash of what it wrote (`composed-hash`), so vibe can
  tell a library change from your own edits. Your `agent-instructions.md` and Claude theme don't
  count: they're carried across.
  - **Not edited since it was composed:** it's re-composed, after a backup (`vibe --restore` rolls
    back), as `vibe -C -f` would.
  - **Edited by hand:** it's left alone, with a warning. `vibe -C` re-composes it when you're
    ready, replacing your edits (backed up first).
  - **Composed before vibe kept the hash:** it's older than this vibe, so it's re-composed (backed
    up first), unless it's identical to a fresh composition anyway.
  - **Not composed from the library** (written by hand, copied, blank, or vibe's own): there's
    nothing to update.
- **A sandbox** is out of date when what it would be built from now differs from what it was
  built from: the kit, the agent, the environment, the published ports, and the folders of this
  machine's it's given (its memories, the review's inbox, the clipboard's history, a NuGet folder).
  Its Claude theme doesn't count. A sandbox built before vibe kept track (of any of these) is out
  of date. Rebuilding carries across
  what vibe knows of (agent memories, theme, GitHub token, timezone, the conversation), but
  anything you installed by hand, or kept outside the workspace, goes with the old sandbox.

**When a session starts**, `vibe` brings its folder's profile and sandbox up to date first. A rebuild
is counted down, on a terminal, for you to change your mind:

```
vibe: sandbox 'my-app' is out of date: what it's built from has changed
rebuilding it in 5s — Enter to go now, s to skip this time, Ctrl-C to quit
```

`s` starts the sandbox as it is; Ctrl-C quits, changing nothing. With no terminal, there's no
countdown. `vibe --no-auto-update` starts the sandbox as it is for one run, and `autoUpdate: false` in
`~/.vibe/settings.yaml` (or a profile's `settings.yaml`) turns all of this off.

**`vibe --update-sandboxes`** does the same for everything at once, starting no session: every
profile in `~/.vibe/profiles` (each once, however many sandboxes share it), then every sandbox vibe
has a record of, each left running or stopped as it was. A sandbox with a vibe attached is left
for its next session start (`vibe --stop-all` first, to update those too). One whose folder no longer
exists is skipped, and left as it is: a sandbox can't be built for a folder that isn't there, so any
rebuild (this, `-r`, `-C` or the Admin tab's) checks first, rather than removing it and failing to
make it again. `vibe --cleanup` deletes it.

Rebuilding is destructive, so it first lists what it will re-compose and rebuild, and asks once; with
nothing out of date, it doesn't ask. `-f` goes ahead without asking, and with no terminal to ask on, it
won't go ahead without `-f`. Once it's going, it never stops to ask: when sbx has a question, the
rebuild fails with sbx's question as the reason. It carries on past a failure, ends with a summary, and
exits non-zero when anything failed, so with `-f` it suits a cron job:

```
vibe --upgrade -f && vibe --update-sandboxes -f
```

**After an install** (`vibe --upgrade` runs one from the new release), vibe lists the profiles and
sandboxes the new vibe takes as out of date, and says what to do: `vibe --update-sandboxes` now, or
leave each to be brought up to date when its next session starts. When sbx can't say what it has, it
says it couldn't check.

## Building

`make` builds `vibe` into the repo root (and `make test`, `make vet`, `make check`, `make clean`
do what they say). Without make, `./make.sh` (bash) and `./make.ps1` (PowerShell) take the same
targets. The binary finds its bundle relative to its own location, so leave it where it is built
and put it on `PATH` with a symlink — or use `vibe --install`.

`make dist` (or `./make.sh dist`) packages a zip in `dist/` holding the binary and that bundle,
for this machine or for whatever `GOOS`/`GOARCH` are set to. Every push to GitHub does this for
Linux, macOS and Windows on both amd64 and arm64 (`.github/workflows/build.yml`), after running
the tests on all three, and attaches the zips to the workflow run. A push to master also
publishes them as a GitHub release, versioned as `VERSION` plus the run number (`0.1.57`,
which `vibe --version` reports too). Setting `BUILD` does the same for `make dist`.

The version lives in one place: the `VERSION` file at the repo root, which holds major.minor
(`1.4`) and is bumped by hand. The third part is the CI run number, so every release is a
valid semantic version newer than the last (`1.4.57`), and GitHub lists the newest first. The
version is embedded into the binary, and `vibe --version` prints it with the commit it was built
from, which `go build` records by itself (`-dirty` when there were uncommitted changes), and the
build date.

## Usage

```
vibe                       # use $PWD, profile derived from its folder name
vibe /path/to/code         # use an explicit folder
vibe -n/--name custom .    # override the derived sandbox name
vibe -p/--profile foo      # use profile "foo" instead of the derived one
vibe -s/--stop [path]      # stop the sandbox for path (default: $PWD); with -f,
                           #    no sandbox there isn't an error
vibe --cancel [path]       # cancel what the companion page for path has under way
                           #    (the agent's turn, an Admin action); the sandbox
                           #    keeps running
vibe -S/--stop-all         # stop every running sandbox, in parallel, showing each
                           #    one's progress (asks first; -f: without asking)
vibe -c/--ssh [path]       # ssh into the sandbox for path
vibe -r/--re-init [path]   # remove and recreate the sandbox from its profile
vibe -r -f/--force         # ...without prompting
vibe -f                    # ...and, for an unknown profile, create a blank one
                           #    instead of asking
vibe -R/--re-create [path] # delete the profile too, then re-init — the profile
                           #    is gone, so this always re-prompts
vibe -N/--no-companion     # ...without serving the companion page this run
vibe --no-start            # ...doing the setup (create, -r, -R, -C) but not starting the sandbox or companion;
                           # prints "type vibe to get started" unless -f is given (handy for cron)
vibe --git-token [path]    # give the sandbox for path a GitHub token to push
                           #    with, renew or change it, or take it away
vibe --git-token-env VAR   # ...a new sandbox (or --git-token) the token in $VAR,
                           #    without asking
vibe --git-rules R[,R]     # ...and these rules for pushing, without asking:
                           #    feature-branch, no-default-branch
vibe --no-git-token        # ...no token for a new sandbox, without asking;
                           #    with --git-token, take its token away
vibe --migrate FROM TO     # move folder FROM to TO, its sandbox with it (see below)
vibe -l/--list             # list every sandbox and its state (see below)
vibe -a/--info [path]      # show the sandbox's settings, its companion page and how to
                           #    reach it from another machine, and, while it runs,
                           #    its uptime and memory and disk use
vibe -H/--headless [path]  # start the sandbox for path and serve its companion page,
                           #    from a vibe in the background (see "Headless")
vibe --headless --foreground
                           # ...staying in the foreground, until Ctrl-C
vibe --restart [path]      # end the vibe already running for path, leaving its sandbox
                           #    running, and take over (asks first if that would
                           #    interrupt anything; -f: doesn't)
vibe --headless --restart  # ...taking over headless
vibe -M/--manage           # serve the Profiles page on its own, until Ctrl-C
vibe -x/--cleanup          # pick profiles and sandboxes to delete, grouped by
                           #    profile, with the likely trash selected
vibe -d/--delete [path]    # pick whether to delete the sandbox for path, its
                           #    profile, or both (-f: both, without asking)
vibe -i/--install          # copy the bundle into ~/.vibe and the binary into
                           #    ~/.local/bin; from a newer release, upgrade
vibe -i -u/--update-strategy merge,keep
                           # ...settling files changed on both sides without
                           #    asking: keep, update, merge,keep, merge,update
vibe -U/--upgrade          # check for newer vibe and sbx releases, pick which
                           #    to install (-f: all of them, without asking)
vibe --update-sandboxes    # bring every profile and sandbox up to date with the
                           #    library, starting no session (see "Keeping
                           #    sandboxes up to date"); asks first, unless -f
vibe --no-auto-update      # start the sandbox as it is, even when it's out of date
vibe -I/--install-sbx      # install the latest stable Docker Sandboxes (sbx);
                           #    on macOS and Windows, asking how (-f: the first
                           #    way, even when up to date)
vibe -v/--version          # print the version, commit and build date
```

Every option has a long and a short form. `-s`, `-c`, `-a`, `-r`, `-R`, `-C`, `-d` and `--git-token` resolve the sandbox
name exactly as a normal run would, so `vibe -s && vibe` restarts whatever you were working on.
`-l` and `-x` work on every sandbox at once and take no path.

`-x`/`--cleanup` is for the profiles and sandboxes a machine accumulates. It lists every profile, with
the sandboxes vibe built from it under it, and then the sandboxes `sbx` has that vibe has no record
of, as a checkbox list (space selects, enter is done). Each line says what's worth knowing when
deciding whether it's trash:

- **A profile:** its features, how many sandboxes it has, and flags: no sandboxes, every sandbox or
  folder gone, out of date, edited by hand, features missing from the library.
- **A sandbox:** its folder, and whether it's running, stopped or gone from sbx, whether its folder
  is gone, whether a vibe is attached, whether it's out of date.

What's likely trash starts selected: profiles with no sandboxes, profiles whose sandboxes are all gone
(from sbx, or their folders), and sandboxes gone from sbx or whose folder is gone. Deselect what you
want to keep.

**Selecting a profile deletes all of it:** the profile, all its sandboxes, their records, agent
memories, clipboard history, conversations and GitHub tokens, and the profile's backups. There's no
undo. **Selecting a sandbox** deletes it and what vibe keeps for it; its profile stays. Either way the
sandbox's record goes, as a record claims its published host ports whether or not anything is
listening, so one left behind would reserve those ports against every sandbox made afterwards.

Before anything is deleted, everything that will go is listed by name and path, and you're asked
once. A failure doesn't stop the rest; a profile is kept when one of its sandboxes couldn't be
removed, so it can be tried again. Some things aren't offered, with a note saying why: a sandbox with
a vibe attached (stop it first), and profiles that ship with vibe, which the next upgrade would bring
back. There is no unattended form of this: `-x` always asks, and `-f` has nothing to skip.

`--migrate FROM TO` moves a folder vibe has a sandbox for, and the sandbox with it. sbx can't point a
sandbox at another folder, so it's rebuilt for the new one, under the name the new folder gives it (a
folder called `TO` gives sandbox `to`). What vibe keeps for it goes along: the agent's memories and the
page's conversation (filed under the new folder's path, which is where the agent looks for them), its
GitHub token, clipboard history, timezone, theme and page address. What a rebuild loses, it loses: anything
installed in the sandbox by hand, or kept outside its folder. `TO` mustn't exist yet (vibe moves the
folder there itself), and has to be on the same disk. It says what it will do and asks first (`-f`
doesn't), and checks everything it can before changing anything; a vibe running in `FROM` has to be
ended first.

`-a`/`--info` shows the sandbox's profile, agent, memory setting, timezone and, for a guided profile, the
library features it was composed from. Once it has had a companion page, it shows the page's address (and
whether a vibe is serving it now), and the `ssh -L` command that reaches it from another machine: it
forwards the page's port and the sandbox's published ports (the clipboard's, which the page embeds), each to the same number,
as the page only answers on its own port. The host name in it is this machine's own; use whatever name
the other machine knows it by. sbx publishes a sandbox's ports on this machine's loopback alone, so
without such a forward, they can't be reached from elsewhere. While the sandbox is running, it also shows how long it
has been up (since it was last started) and the memory and disk it is using, read from inside it:

```
sandbox   vibe
folder    ~/code/opensource/vibe
profile   vibe
agent     claude
memory    12g
timezone  Africa/Johannesburg
features  go, node
status    running
uptime    3h 35m
mem used  1.9 GiB of 11.8 GiB (16%)
disk used 1.5 GiB of 19.5 GiB (7%)
docker    340.0 KiB of 48.9 GiB (0%), on a disk of its own
```

"mem used" counts what the sandbox can't hand back on demand, so the page cache isn't included.
It's a snapshot, so check it while the sandbox is at its busiest (building, running tests) before
deciding to give it less memory. "disk used" covers the sandbox's image and everything written
over it. Docker inside the sandbox keeps its images on a separate disk, shown as "docker". A
stopped sandbox isn't started just to answer: you get its settings only.

`-d`/`--delete` is for one folder's sandbox and profile. It offers both in a checklist:

```
[x] remove the sandbox <sandbox name>
[ ] remove the profile <profile name>
```

The sandbox starts selected, because the next `vibe` in that folder rebuilds it. The profile starts
deselected, because it can hold work that's harder to redo. If other sandboxes use the profile, its
line names them. Only things that exist are offered. A profile that only ships with vibe has no
copy in `~/.vibe` to delete, so it isn't offered. `-f` deletes both without asking. With no
terminal and no `-f`, nothing is deleted. vibe logs what it removed and what it kept. As with
`-x`, removing the sandbox drops vibe's instance record for it too.

`-l`/`--list` prints one line per sandbox, in four groups: `RUNNING`, `UNKNOWN` (sbx reports a
state other than running or stopped), `STOPPED`, then `GONE` (vibe has a record of it but sbx no
longer has the sandbox). Within each group, names are sorted ignoring case:

```
RUNNING  vibe  phoenix
UNKNOWN  sbx   hand-made
STOPPED  vibe  crit
GONE     vibe  old-project  /home/me/code/old-project
```

The columns are state, owner (`vibe` if vibe made the sandbox, `sbx` if it was made with sbx
directly) and name. `GONE` lines also give the sandbox's folder. Every state is seven characters
wide, so the name always starts at column 16. To get just the names of running sandboxes, use
`vibe -l | awk '$1=="RUNNING" {print $3}'`. The state is coloured only when the output is a
terminal and `NO_COLOR` isn't set. Notes about `UNKNOWN` and `GONE` sandboxes, such as what sbx
said or `--cleanup`, go to stderr, so scripts don't see them.

`-c`/`--ssh` requires `sbx setup ssh` to have been run once on this machine.

While vibe runs, it titles your terminal tab `VIBE: <folder>` (e.g. `VIBE: ~/code/vibe`), so you
can tell which tab is which project. Most terminals show this as-is, including Konsole, Terminator,
Windows Terminal and iTerm2. Konsole only shows it if its tab title format includes `%w`. Inside
tmux or screen, it names the pane unless they're set to pass titles on. Terminals that can save a
title (xterm, GNOME Terminal and others) get theirs back when vibe exits. Elsewhere vibe's title
stays until something else, usually your shell's prompt, sets another. Sandboxes created for
Claude Code get `CLAUDE_CODE_DISABLE_TERMINAL_TITLE=1`, so Claude doesn't replace the title with
the conversation's. Set it to `0` in a profile's `env` to let Claude set it again.

## I don't have access to a paid model - can I still use vibe?

Yes! Select OpenCode as the agent during setup, and once the sandbox has started, select a
free model with the `/models` command. Zen and Miro models should work.

## Where the configuration lives

There are two layers. `~/.vibe` (override with `$VIBE_HOME`) is the master copy, and the bundle
next to the binary is the fall-back — so a vibe upgrade replaces the bundle without touching
anything you've changed.

On its first run vibe seeds the overlay: it creates `~/.vibe`, copies the bundle's `config.yaml`,
`settings.yaml` and the whole `defaults/` tree there, then asks, one at a time, whether to copy
each bundled profile across. Decline and the profile still works — it's just read from the bundle
until you take a copy.

```
vibe: first run — setting up /home/me/.vibe
vibe: copy profile 'valleyrunner' into /home/me/.vibe/profiles? [y/N] y
vibe:   copied config.yaml, settings.yaml, defaults/
vibe:   copied profile 'valleyrunner'
```

After that:

- **Single files override one at a time, by relative path.** `~/.vibe/settings.yaml` replaces the
  bundle's — it is not merged into it, the whole file wins. Same for `config.yaml`.
- **`defaults/` and each profile override whole.** Once `~/.vibe/defaults/` exists, that directory
  *is* the defaults: the bundle's is ignored entirely, so a script you delete from your copy is
  gone rather than reappearing from underneath, and one you add joins the sequence in number
  order. `~/.vibe/profiles/valleyrunner/` works the same way against the bundle's profile of that
  name. The trade is that new default scripts shipped by a vibe upgrade don't reach your copy on
  their own — diff `~/.vibe/defaults` against the bundle's after upgrading.
- **Profiles vibe creates land in `~/.vibe/profiles/`.**

## A folder with no profile yet

The first time you run `vibe` in a folder, the derived profile usually doesn't exist yet. Rather
than failing, vibe offers to create it:

```
vibe: no profile 'foo-browser' yet in /home/me/.vibe/profiles
vibe: create it how?
❯ guided profile creation
  copy an existing profile
  create a new blank profile
(↑/↓ to move, enter to select, q to quit)
```

Copying clones the chosen profile's whole directory — config, settings, install scripts and agent
files — and renames it, which is the quickest way to start from something close to what you need.
A blank profile is just a `config.yaml` naming it, plus empty `install-scripts/` and
`agent-files/` directories to grow into; the sandbox it builds is the base kit and nothing more.

Either way the new profile is written to `~/.vibe/profiles/<name>/`, and copying reads the source
profile from wherever it currently wins — your overlay if you have a copy of it, the bundle
otherwise.

`vibe -f` skips the question and creates a blank profile; `vibe -p <existing>` uses a profile you
already have without creating anything. With no terminal to ask on, vibe says so instead of
guessing.

### Claude colour themes

Every sandbox ships vibe's Claude Code colour themes, Amber, Blue, Cyan, Green, Orange, Purple,
Red and Yellow, from `defaults/agent-files/.claude/themes/`. They keep Claude's reply text white and
colour the accents: highlighted `code` in replies, the lines around your input, Claude's spinner
and the mode indicators. The exception is "bypass
permissions", which uses the same colour as real errors and so stays red. `/theme` switches
between them at any time.

Creating a sandbox asks which one the agent starts with. The list is exactly the themes in
`~/.vibe/defaults/agent-files/.claude/themes/` (or the bundle's defaults, if there's no
`~/.vibe/defaults`), so adding or deleting a theme file there changes what is offered. A
`~/.vibe/defaults` seeded before themes existed has none, and the question is then skipped; copy
the bundle's `themes` folder in to get them. A profile's own `agent-files/.claude/themes/` are
deployed as well, and `/theme` can switch to them, but they aren't offered here:

```
vibe: select claude theme
❯ default
  amber
  blue
  cyan
  green
  orange
  pink
  purple
  red
  yellow
```

The answer is recorded in the profile's folder as `claude-theme`. A rebuild (`-r`, `-R` or `-C`)
doesn't ask again. It reads the theme the old sandbox's agent is actually set to, including any
change made with `/theme` since, before removing it. If it can't, it uses what the profile
recorded, and it asks only when neither is known. `-f`, or running without a terminal, takes that
answer, or Claude's default, without asking.

### Timezone

Creating a sandbox asks which timezone it keeps its local time in, starting on this machine's
(from `TZ`, `/etc/localtime`, or on Windows, its timezone setting mapped to the matching IANA
zone). There are a few hundred zones, so the list scrolls: type to narrow it (`johan` finds
`Africa/Johannesburg`), ↑/↓ to move, enter to choose and Esc to keep the default. `-f`, or
running without a terminal, takes this machine's without asking.

The answer is recorded in the sandbox's `instances/<name>.yaml`, and vibe puts the sandbox back on
it at every session start, so it holds even if something in the sandbox changes it. That means
pointing `/etc/localtime` at the zone, writing `/etc/timezone`, and exporting `TZ` from
`/etc/sandbox-persistent.sh`. The last of these matters most: sbx starts the sandbox's processes with
`TZ` set to a bare offset (`SAST-2`, say), which overrides `/etc/localtime` and names no zone, so
anything asking for the zone's name (Node's `Intl`, .NET's `TimeZoneInfo.Local`) gets nothing
useful. Every shell the agent runs sources that file first, so tests and tools it runs see the
real zone. `-C/--re-compose` and `-R/--re-create` ask
again, starting on the recorded zone; `-r/--re-init` keeps it. A sandbox from before vibe recorded
one is put on this machine's.

### Status band

Every sandbox also ships `vibe-band`, a Claude Code plugin from
`defaults/agent-files/.claude/skills/vibe-band/` that draws a line above the prompt: the sandbox
name, the project folder (the workspace, which doesn't follow the agent's `cd`), and git status in powerline style (branch, then `+` staged, `~` modified,
`?` untracked, `✘` conflicted, `↑`/`↓` ahead/behind, `#` stash count). It refreshes every 2 seconds and after each
turn. A second line, `copy-paste at: <url>`, shows the
clipboard page's address (from `clipboard-url`), or `copy-paste at: (starting)` until the clipboard server is
listening and vibe has confirmed its host port for this boot. Claude Code loads it automatically from `~/.claude/skills/`, so nothing needs configuring; delete
the folder from `~/.vibe/defaults/agent-files/.claude/skills/` to leave it out. As with themes, a
`~/.vibe/defaults` seeded before it existed won't have it until the bundle's copy is added.

### Companion page

Whenever vibe opens a sandbox it also serves a page for it on your own machine, prints its address
(`companion page: http://localhost:10000/#…`) and opens it in your browser. It is what you'd otherwise
reach for the sbx TUI for, scoped to the sandbox you're working in. Its header shows what the infobar
in the sandbox does — the sandbox, its folder, and the folder's git branch and status — and below that
are tabs. When the agent uses one of vibe's Claude themes, the page picks up its colour, subtly: a stripe
along the top, the project's name, the chosen tab and focus outlines, so you can tell which sandbox's page
you're looking at:

- **Conversation** — talk to the agent from the page, with the sandbox's clipboard page (see
  `clipboard-bridge` below) embedded beside it: paste images and files anywhere on the tab and they go
  to the clipboard. The prompt box grows as you type, up to half the column. The agent's replies, and your prompts, are shown as Markdown — headings, lists, tables, code — built
  as text, so nothing in them can become markup on the page; links open only to this machine's own pages. A block of code has a copy button (two pages) at its top right. A review comment named by its number (#24) is a link to it on the Review tab.
  Your prompt is sent exactly as you typed it; only how it's shown changes, so fence pasted console output
  in ``` (and put globs like `*.go` in backticks) to keep it as it is. **My prompts as Markdown**, above the
  conversation, turns that off for your prompts, in that browser, if you'd rather see them as you typed them.
  Each message shows the time it was said, and each prompt you send carries ticks like a chat message: one grey tick once vibe has
  it, two grey ticks once it has reached the sandbox, and two blue ticks once the agent has started
  answering. If it has been answering for an hour without a word, the pane says it may be stuck (it's
  left running: stopping it is up to you, with **Stop**, or `vibe --cancel` from a terminal). For as long as it is answering, three dots below its latest message show it's still at it,
  as in a chat app. When the agent's context is full, it first summarises the conversation so far to make
  room, which can take a few minutes with nothing else to show: the pane says so while it does. Typing `/` at the start of a prompt lists the agent's slash commands, filtered as
  you type with the closest match picked: Tab (or Enter) completes it, the arrow keys pick another.
  vibe learns the list from the agent as it starts, so it shows once the agent has answered a prompt.
  Until vibe sees the clipboard page answer the pane shows a placeholder; if it still isn't
  up after 30 seconds, a **Start it** button runs the kit's `start-clipboard` in the sandbox (harmless
  if it is already running). The conversation, and what you are typing, outlive vibe: start it again
  and the page carries on where it was. vibe also keeps a copy of the agent's record of the session
  (in `~/.vibe/companion/`), and gives it back to a sandbox that has been rebuilt (by `--re-compose`,
  say), so the new agent remembers it too. The page's tab icon is the sandbox's agent's, and it says
  what the agent is up to even while you're on another tab: a grey dot pulses while it works, and a
  blue dot shows it has finished, until you come back to the page. The Conversation tab does the same
  while you're on another tab, until you open it. Once the agent has answered you can
  send the next prompt, even if it left something running that it is waiting on (a review page, say):
  sending ends that. Links to pages on `localhost` in what the agent says can be clicked.
- **Review** — the changes in the sandbox's folder, worked out on this machine, read with go-git by a helper process (vibe itself, held to a memory limit and stopped when its time is up), which runs nothing the repository's own config names, since the sandbox can write that; no git is needed on the host. It covers uncommitted changes (
  untracked files with them; or a branch's, or a commit's), with the agent's review comments under the
  lines they're about: side by side (or in one column), with the hidden lines between changes a click away,
  and a tree of the changed files beside it (a quarter of the width; drag its edge to resize) with a filter
  that keeps the files whose path has every word typed. Ask the agent for a review and it uses `vibe-review` (and its skill) to comment,
  set a reading order, and say when it's done. Reply, resolve, dismiss or reopen a comment there (Ctrl+Enter replies and, with **Next after reply** ticked in the bar, goes on to the next comment, ready to reply to it); **Ask
  the agent** sends your reply as a question, which it answers on the comment (the turn shows in the
  conversation too; three dots show on the comment while it answers). Comment on a line yourself with the **+** over its number, on changed lines and on lines opened from a gap alike (Shift-click a second **+** to comment on the lines between, or drag a **+** onto another line's to move the comment you're writing there; Write, with a formatting toolbar and Ctrl+B / Ctrl+I / Ctrl+K, or Preview it as
  Markdown): your comments and replies wait, editable and deletable, until **Submit to agent** sends them
  all as one turn, and takes you to the Conversation tab. The agent answers on each, and changes code only where a comment asks it to. A comment of yours can still be edited after it's sent: it's marked **edited** (click that to see what it said), and goes to the agent again with the next Submit, saying what changed; it can no longer be deleted. Files are shown in the tree's order (folders first, each by name), so the tree, the files and the comment arrows all go the same way; the agent's reading order is shown as a numbered step on its files, in the tree and in their headers. Code is coloured by its language (worked out on this machine, a file at a time as you scroll to it; a file over 10,000 lines or 1 MB is left plain, saying so). Each file in the tree has an icon for what happened to it (added, changed, deleted, renamed) and how many comments it has; the bar says which comment you're at ("3/5 comments"), with arrows to step through them, and a speech-bubble button that slides a pane of every comment in over the diff: short cards (where, its state, how it starts, its replies), filtered to All, Open or Yours, ordered by file path or agent reading order (the same setting as under the count, kept in step), or newest first; click one to go to it. The arrows go in path order, as the files are shown, or (click the count to choose) in the agent's reading order, which this browser remembers. An image or file pasted on the tab goes to the clipboard page, to reach the agent with your next message (the bar says how many are waiting); text pastes as text. A comment on a line the diff doesn't show is shown under that line all the same, with the lines around it opened. With no review open, the tab offers to start one: of the uncommitted changes, of this branch against the default one, or of the commits not yet pushed; either asking the agent to review it (the diff shows at once, its comments as it makes them) or just showing the diff, to review yourself. **Close review** finishes with one (it's kept, and can be reopened); a review whose diff has nothing left in it, say once its changes are committed or pushed, closes by itself (not while the agent is at work on it, nor once you've reopened it). The tab works in the top of the folder's git repository: start vibe there, as in the sandbox, which has only its folder, the agent can't use git on a folder below the top (vibe says so when it starts). A comment whose lines read differently now is marked as such. When lines above a comment come or go, it follows its code to its new line, and says where it was made. **Hide whitespace changes** in the bar leaves out what changed only in indentation or spacing (this browser remembers it). Tick a file **Viewed** to fold it away and dim it in the tree ("3/8 viewed"); if its change changes after, it's unticked and says so. When the agent
  updates the review (a comment, an answer, finishing it), a dot shows on the tab until you open it, or
  click or type anywhere in it if it's already open. A review that can't be read (damaged, or grown too large) says so on the tab, with **Start afresh**, which sets it aside in the review folder's `history/` and begins an empty one. Reviews are kept in
  `~/.vibe/reviews/<sandbox>/`, so they outlive the sandbox stopping or being rebuilt; `--migrate` takes
  them along, and deleting the sandbox deletes them. A sandbox built before this has no review folder
  until it's rebuilt.
- **Network log** — every host the sandbox has tried to reach, with hit counts and whether it was
  allowed or blocked, refreshing every couple of seconds while the tab is showing. Filter by status or
  search for a host. A blocked host has an **Allow** button, an allowed one a **Block** button; click
  a row to see everything sbx said about it.
- **Network rules** — the rules that apply to the sandbox, a box to allow or block a host, and
  **Remove** for the sandbox's own rules.
- **Filesystem rules** — the same, read-only (sbx doesn't log filesystem access yet).
- **Admin** — what `vibe --info` shows of the sandbox, and what you'd otherwise go to a terminal for:
  start or stop it, rebuild it from its profile (`-r`), re-compose it with features added or taken away
  (`-C`), restore a backup of its profile (`--restore`), and save the agent's memories now. It also says
  whether newer releases of vibe or Docker SBX are out (with a dot on the tab when they are; hovering over the tab says which), and shows
  the sandbox's GitHub token, read-only, with a link to make or renew one on GitHub. Each action shows the
  command it matches and asks first, and nothing that takes the sandbox away runs while the agent is
  answering. An action taking far longer than it should (3 minutes for start, stop or saving memories,
  15 for a rebuild) is flagged as possibly stuck, with what to check, while vibe keeps waiting for it;
  **Cancel** on the action's card ends it (a rebuild asks first, as it may leave the sandbox half-made),
  and the card then says what state the sandbox is in. `vibe --cancel` does the same from a terminal.
  When the sandbox is stopped or rebuilt, from here or with `vibe --stop` or sbx, vibe keeps
  serving the page; the terminal session attaches again once the sandbox is back (Ctrl-C ends vibe).
  It also edits your [instructions to the agent](#your-instructions-to-the-agent): **Save** asks what to do when the file has
  changed on disk since (merge, overwrite, or reload), and **Optimise** asks the agent for a tidier
  version, leaving out what's repeated or already in the universal file, for you to **Accept** or
  **Revert** (Ctrl-Z works throughout). While a session is attached, vibe keeps its messages out of the
  terminal, which is the agent's screen then, and prints them when the session ends; the terminal's
  title says when the agent is optimising.
- **Profiles** — every profile on the machine, with the sandboxes built from it, to clean up what's
  trash: the same as [`vibe --cleanup`](#usage), with the same flags and the same rules. The filter (`/`
  gets to it, Esc clears it) narrows the list to what matches every word typed: names, folders,
  features, and words like `running`, `stopped`, `out of date` or `trash`; a profile whose name matches
  shows all its sandboxes. Selections the filter hides still count, and are listed when deleting. **Select likely
  trash** selects profiles with no sandboxes, profiles whose sandboxes are all gone, and sandboxes gone
  from sbx or whose folder is gone; **Delete selected…** lists exactly what will go, by name and path,
  and asks once. Selecting a profile deletes all of it, with its sandboxes. What can't be deleted says
  why: this page's own sandbox, a sandbox with a vibe attached, and profiles that ship with vibe. Each
  row also has what can be done to it: a sandbox can be started, stopped (as `vibe --stop` does), rebuilt
  (and left running or stopped as it was), or updated when it's out of date; a guided profile can be
  re-composed (its sandboxes rebuilt after) or, when it or a sandbox of its is out of date, updated as
  `--update-sandboxes` would. This page's own sandbox is the Admin tab's to work on, and one with a vibe
  attached can only be stopped. Each action runs as the page's one action at a time, with **Cancel**,
  like the Admin tab's, and anything that rebuilds asks first.

Every change shows the exact `sbx` command it will run and waits for you to confirm. A change only ever
adds or removes a rule scoped to *this* sandbox: global and org rules are listed but not touched, and a
rule that would match every host (`*`, `**`), or far more than one site (`*.com`, `*:443`, `0.0.0.0/0`), is
refused — that is for the sbx CLI. The network log offers **Allow** only for a single host: what's logged is
whatever the sandbox asked to reach, so it can be a pattern of the agent's making.

The page needs `sbx` to be signed in (`sbx login`), the same as `sbx policy` does; until then it says so.

Its address is the same at every start (the port is kept in the sandbox's instance record, and so is a
secret token, in `~/.vibe/companion/<name>.token`), so a bookmark keeps working. The page lives as
long as vibe does: when the session ends it shows "vibe is not running for this sandbox", and
reconnects by itself when you run vibe again. So that's hard to miss, the tab's icon gets a red cross
and the page goes behind frosted glass saying so, within a few seconds, even in a background tab;
**Dismiss until it reconnects** clears the glass (the cross and the status top right stay). Both go
when vibe is back. A second vibe for the same sandbox leaves the first's page
be and just prints its address.

Because the page can change what the sandbox may reach, it is built to be out of the sandbox's reach: it
listens on `127.0.0.1` only, answers only requests for `localhost`/`127.0.0.1`/`::1` on its own port (so
not DNS rebinding, and not anything sent through `host.docker.internal`), needs the token on every call
(it rides in the URL's `#fragment`, which browsers keep from servers and logs), takes changes only as
same-origin JSON POSTs, and shows host names as text, never markup. The token is never written into the
sandbox.

**Headless.** `vibe --headless [path]` (`-H`) is the companion page with no terminal attached: for a
sandbox that already exists (run `vibe` there once first, as creating one asks a few things), it brings
it up to date as a session start does, starts it if it's stopped, runs its start-up, and serves its
page, which is then where you work with it.

It does that from a vibe it leaves running in the background: what needs your terminal (another vibe
in the folder, the countdown to rebuilding an out-of-date sandbox) happens first, then it waits for
the background vibe to say its page is up, prints the page's address and where it logs
(`~/.vibe/logs/<sandbox>.log`, started afresh each time), and gives you your terminal back. If the
background vibe can't get going, the end of its log is shown, and it exits non-zero. Running
`vibe --headless` again while one is running just points at it. To use the page from another machine,
`vibe --info` gives the `ssh -L` command that forwards its ports there. The agent has to be logged in
already: nobody sees its login screen. Each time the sandbox starts, a headless vibe checks, and warns in
its log if it isn't (when sbx leaves logging in to the agent; when sbx supplies credentials itself, vibe
can't tell, and says nothing). A prompt from the page that fails for it says what to do: `vibe` in the
folder, in a terminal, then `/login` there. Plain `vibe` in the folder attaches a
terminal alongside it, without asking: the headless vibe carries on serving the page, and an
out-of-date sandbox isn't rebuilt under it (`vibe --restart` does that). Rebuilding or deleting (`-C`,
`-r`, `-R`, `--delete`) still asks what to do about it first.

**`--restart`** ends whatever vibe has the folder (in a terminal or headless, saving its agent's
memories, and leaving the sandbox running) and takes over: `vibe --headless --restart` starts a fresh
headless vibe, to pick up a new vibe after `--upgrade` say, and `vibe --restart` a terminal session,
on a sandbox that was headless too. It asks first when that would interrupt something: another
terminal's session, or an agent part-way through a turn (it asks the other vibe's page). `-f` doesn't
ask; with no terminal and no `-f`, it says what would be interrupted and doesn't. A folder held for a
rebuild isn't restarted. `vibe --stop && vibe --headless` restarts the sandbox too. `--foreground` keeps it in the
foreground instead, for tmux, `nohup` or a systemd user unit.

It carries on until another vibe asks it to stop (or, in the foreground, Ctrl-C or SIGTERM), and then
saves the agent's memories (after Ctrl-C, it leaves the sandbox running).
`vibe --stop` ends it too, then stops the sandbox: with no terminal, nobody would see it carry on
without one, still running an old vibe after an `--upgrade`, say. Stop on the Profiles page does the
same. Stop on its own page's Admin tab stops just the sandbox and keeps the page up, as you're looking
at it; its Start brings the sandbox back, and vibe runs its start-up again. sbx itself stops a sandbox
about a minute after nothing is attached to it. So while the page is in view, it keeps the sandbox
awake (a tiny command in it every 20 seconds), and so does vibe while the agent is at work, answering
or with something it left running (a review page, say), page in view or not; otherwise, out of sight, it
lets sbx stop it, and starts it again
as soon as you're back (unless it was stopped on purpose). The page's own background checks never wake
a stopped sandbox, and the clipboard pane keeps its page while its server is away, picking up again by
itself when it's back. A prompt sent while the sandbox is stopped starts it first, saying so in the
conversation, and is answered once it's up (or says why it couldn't start, after three minutes).

**`vibe --manage`** serves the Profiles tab on its own, for no sandbox or folder: every profile and
sandbox, to clean up, without a session open. It keeps its own port and token (in
`~/.vibe/companion/manage/`), so its address stays the same, and serves until Ctrl-C. Run it again while
it's being served, and it opens the one already there.

`companion:` in `settings.yaml` (bundle or profile) sets what happens by default: `open` (the default —
serve it and open the browser), `serve` (serve it and only print the address — for when you have a bookmark,
or no desktop), or `off`. `-N/--no-companion` turns it off for one run. vibe doesn't open a browser
over ssh, or on Linux without a display; it prints the address instead.

### CPUs a sandbox gets

`sbx` gives a sandbox every CPU of the host's unless told otherwise, so a build in it (`dotnet build`
with all cores, say) can leave your machine nothing to run vibe, your browser and `sbx` itself on:
the companion page then goes slow, and may say vibe has disconnected. vibe tells `sbx` how many to
give (`sbx create --cpus`): the `cpus` setting, and when there's none, **half the machine's**,
rounded down. A new sandbox asks, offering any number from 1 to one less than the machine has, so
the host always keeps one. It's the same question as the memory one: `-f`, or no terminal, uses the
setting, or half. A `cpus` more than the machine can spare is lowered to the most it can.

The choice isn't remembered per sandbox, as memory's isn't: a rebuilt sandbox gets the setting (or
half) again, so set `cpus` in `settings.yaml` (or a profile's) to keep a number. A sandbox already
built keeps the CPUs it has until it's rebuilt.

### Letting the agent push

Out of the box, the agent in a vibe sandbox has no git credentials: it can commit, but you push — and
so you read the code first. If you'd rather hand the agent a plan that takes a few commits and let it get
on with it, a new sandbox can be given a GitHub token. It's opt-in, and never required: creating a
sandbox asks, and no is the default answer.

- **no** — you commit and push yourself.
- **your default token** — the one `vibe --setup` stored, if you stored one.
- **a new token just for this repository** — vibe asks how long it should last (7, 30 or 90 days;
  7 to start with), then opens GitHub's page for a new fine-grained token, filled in with a name
  (`vibe <sandbox>`), the repository's owner, that expiry and the permissions the agent needs
  (Contents, Pull requests and Workflows: read and write). GitHub doesn't let a link choose the
  repository, and its form starts on *All repositories*, so vibe lists what's left for you to do there
  first, and waits for enter before opening it: change *Repository access* to *Only select
  repositories* and choose the repository, check the permissions, generate the token, and paste it
  back into vibe (each picked out in its own colour, unless `NO_COLOR` is set).
- **a token you already have** — paste it.

Whichever you pick, the token shows as a `*` per character as you paste it (so a paste that came out
short, or twice over, is plain to see; a token pasted twice is caught and asked for again), and vibe
checks it with GitHub. It shows whose it is and when it expires, and checks it can push to the folder's
repository (the `origin` remote) by asking for what `git push` asks for first, which changes nothing.
For a token GitHub turns down, or one that can't push there, vibe asks what now: *try again* (paste
another), *use it anyway*, or *continue without a GitHub token*. A classic token (`ghp_…`) or the one `gh auth token`
prints (`gho_…`) works, but reaches every repository your account can, so vibe warns you about it.
A fine-grained token that can push is also checked for the owner's other repositories it was given.
GitHub can't be asked that directly: its repository list shows every repository the token can read,
and every token can read every public repository, your organisations' included. So vibe reads that
list for the owner's repositories alone: a private one the token can see at all is one it was given,
and each public one gets the same harmless push check, several at a time (up to 300 of them; vibe says
if there were more). If the token reaches any of them (it was left on *All repositories*, or given an
extra one), vibe warns, lists them, and asks what now: narrow it on GitHub (editing a token's
repository access doesn't change the token) and have vibe check again, use it as it is, try a
different token, or continue without one.

vibe then asks what the agent should be told about pushing, beyond never force-pushing, which it is
always told: *only work in a feature branch*, and *never push the default branch* (open a pull request
instead). Both start deselected. These are instructions to the agent, not enforced by anything. If you
need the default branch protected, set up a branch rule on GitHub.

The token never enters the sandbox. vibe gives it to sbx as the sandbox's own secret
(`sbx secret set github --sandbox <name>`, on stdin), and sbx's proxy adds it to the sandbox's requests
to GitHub, so `git push` over HTTPS and `gh` both work in there. The agent's rules go in
`~/.config/vibe/git-push.md` in the sandbox, and every sandbox's agent instructions point it there.
If `origin` is an SSH URL, the agent is told to push over HTTPS, since the proxy only covers HTTPS.

vibe remembers when the token expires (GitHub says, when vibe checks it). From three days before, every
session start says so; once it has expired, the session start asks whether to renew it there and then
(with `-f`, or no terminal, it only says so). Renewing opens GitHub's list of your tokens: click the
sandbox's, then *Regenerate token*. That gives a new value with the same repository and permissions,
so there's nothing to pick again; choose how long it should last, and paste it into vibe, which checks
it and hands it to sbx. A running sandbox picks up the new token straight away. The agent's rules
carry the expiry date too, and tell it that if GitHub stops taking the token, it should stop and ask you
to run `vibe --git-token`, not look for another way to push. If the token has gone from GitHub's list,
make a new one instead.

The default token `--setup` stores lasts 30 days unless you pick otherwise, and `--setup` renews it
the same way. Renewing or replacing it offers the new value to every sandbox that was given the
default, since the token they hold stops working once it's regenerated. A new sandbox isn't offered an
expired default.

`vibe --git-token [path]` gives an existing sandbox a token, renews or changes the one it has, or
takes it away. Renewing is its first choice, picked to start with when the token has expired or is
about to. A rebuild
(`-r`, `-R`, `-C`) keeps the token and rules the sandbox had; `--delete` and `--cleanup` remove them.
`vibe --info` shows whether a sandbox has one. With no one to ask (`-f`, or no terminal), a new
sandbox gets no token unless `--git-token-env VAR` names an environment variable holding one (never
the token itself on the command line, where anyone on the machine can read it). That token is
checked the same way, and turned down if GitHub won't let it push. `--git-rules` picks the rules.

### Checkbox prompts

Anywhere vibe asks you to select several things — the guided feature picker, `-x`/`--cleanup`, `-d`/`--delete` —
enter doesn't answer straight away (`--upgrade`'s list is the exception: it just goes ahead). What you selected is listed back, one item per line, and the
list is still there to go back to:

```
Confirm selection:
  - dotnet: the .NET SDK
  - redis: the Redis server

Continue? [Y/n]
```

Enter takes the default and goes ahead; `n` returns to the checklist with everything still
checked, so fixing a near-miss costs one keypress instead of the whole selection; Esc or Ctrl-C
abandon the prompt altogether.

## How a sandbox is built

`vibe` is a single self-contained binary; everything else it needs lives alongside it in the
unpacked bundle, and `~/.vibe` overlays that bundle file for file (profiles directory for
directory):

```
vibe                  # the binary
readme.md
settings.yaml         # base settings — memory, published ports, etc.
config.yaml           # base sbx kit fragment — permissions/ports/env shared by every profile
defaults/             # tooling applied to every profile, regardless of tech stack
  install-scripts/
  agent-files/
library/              # selectable features a guided profile is composed from
  <feature-name>/     # same shape as a profile — see "Library features"
  base/               # features every guided profile always gets (clipboard-bridge)
profiles/
  <profile-name>/
    config.yaml          # profile-specific kit fragment, merged over the base one
    settings.yaml         # profile-specific settings, merged over the base ones
    install-scripts/      # profile-specific setup steps, run after defaults'
    agent-files/           # profile-specific files, deployed after defaults'
    agent-instructions.md # your instructions for this profile's agent, loaded every session
    kit-instructions.md   # the features' instructions (guided profiles; re-composing rewrites it)
agent-instructions.md     # your instructions for every sandbox's agent, loaded every session
```

On each run, `vibe` derives the sandbox's **profile** (`--profile`, else `--name`, else the
target folder's leaf name) and generates a kit spec for `sbx create --kit` from:

- `config.yaml`: base merged with the profile's (maps merge recursively, lists append, and any
  other value the profile sets wins).
- `settings.yaml`: same idea, but for `memory`, `agent`, `publish` and friends — see below.
- `setup.install`: every script under `defaults/install-scripts/`, in their own numeric order,
  followed by every script under `profiles/<name>/install-scripts/`, in theirs. The defaults
  always run to completion before the profile's scripts start.
- `setup.files`: every file under `defaults/agent-files/`, then `profiles/<name>/agent-files/` —
  each deployed at the same relative path under `/home/agent` inside the sandbox. A profile file
  at the same path as a default file overrides it.
- `startup`: if either agent-files tree provides `.local/bin/on-start`, it's added as a
  backgrounded root step. Make it idempotent — it also gets re-invoked (harmlessly) on every
  `vibe` attach, to work around `setup.startup` not firing on a sandbox's very first boot.
- `agentInstructions.content`: the base kit's content, with the profile's `kit-instructions.md`
  appended. sbx offers these to the agent as reading for when a feature is relevant.

### Your instructions to the agent

Two files are yours, for standing instructions the agent should always follow:

- `~/.vibe/agent-instructions.md`: for every sandbox (`vibe --install` makes it, empty).
- `~/.vibe/profiles/<profile>/agent-instructions.md`: for that profile's sandboxes (made, empty, with
  the profile).

They're merged, the universal file first, under "Universal instructions" and "Project-specific
instructions" headings, and a blank file is left out. Unlike the features' instructions, the agent
loads them at the start of **every** session, including each prompt from the companion page. For
Claude, vibe writes the merged text to `~/.claude/vibe-instructions.md` in the sandbox and imports it
from `~/.claude/CLAUDE.md`; Gemini gets the same through `~/.gemini/GEMINI.md`, and Codex gets the text
itself in `~/.codex/AGENTS.md`. vibe's part of those files sits between `<!-- vibe: instructions … -->`
markers, and the rest is left alone: the agent writes there too, when asked to remember something.
Other agents don't load them yet.

They're applied whenever a sandbox starts, and straight away to running sandboxes when you save one
from the companion page's Admin tab. A session already open in the terminal picks changes up when it
restarts, or when you ask it to re-read `~/.claude/vibe-instructions.md`.

A guided profile from before this keeps the features' instructions in its `agent-instructions.md`:
the next time it's used or re-composed, they move to `kit-instructions.md` and an empty
`agent-instructions.md` takes their place. A hand-made profile's `agent-instructions.md` was its
author's own, and becomes its project instructions as it is. Re-composing carries the profile's
`agent-instructions.md` across.

## Writing a profile

A profile only needs `config.yaml` to exist. Everything else is optional — which is why vibe can
offer to create a blank one for a folder it hasn't seen before.

**Install scripts** (`install-scripts/NN-name`) run as a shell script, in filename order. Add
directives as `# vibe: key: value` comment lines anywhere in the file:

- `# vibe: description: ...` — shown while the step runs (defaults to the file name)
- `# vibe: user: 1000` — which user runs the step (defaults to `0`, root)

An install script must be self-contained: `setup.files` are not on disk yet while `setup.install`
runs, so a step that calls `/home/agent/.local/bin/<helper>` from an `agent-files` tree fails with
"not found" every time. `agent-files` scripts are for startup (`on-start`) and for the user to run
by hand.

**Agent files** (`agent-files/<path>`) are deployed verbatim to `/home/agent/<path>`. Directives
work the same way, plus:

- `# vibe: mode: 755` — deployed file mode (defaults to `0755` for anything with a shebang or
  under a `bin/` directory, else `0644`)
- `# vibe: onlyIfMissing: true` — only deploy if the destination doesn't already exist (defaults
  to always overwriting)
- `# vibe: startup: false` — for a script in `.local/bin`, keep it out of the generated `on-start`
  (defaults to true, so a feature's service scripts are started without having to say so). Mark a
  helper the agent or the user runs on demand — like the clipboard's `clipboard-url` — with this.

For a file whose format can't carry a `#` comment (JSON, binary, ...), put the same `key: value`
lines in a sidecar file named `<file>.vibe` next to it instead — e.g. `settings.json.vibe`. The
sidecar itself is never deployed.

Directive lines (inline or sidecar) are stripped from the deployed content, so they never leak
into a real script or config file.

**`settings.yaml`** merges these fields (all optional):

```yaml
memory: 12g          # sandbox memory limit; a new sandbox asks, starting here, capped at
                     #    half the host's memory
cpus: 8              # CPUs a sandbox gets; a new sandbox asks, from 1 to one less than the
                     #    host has. Unset: half the host's (sbx alone would give all of them)
agent: claude         # which coding agent to run
nugetDir: ~/.nuget    # mounted into the sandbox and symlinked in, if it exists on the host
memoryRoot: ~/.vibe/memories   # where per-sandbox agent memories are backed up
defaultFeatures:      # library features a guided profile starts with selected
  - dotnet
env:                  # extra environment variables passed to `sbx create -e`
  SOME_VAR: value
publish:              # ports to publish, and (optionally) a stable URL env var for each
  - name: clipboard
    ports: [5390]
    urlEnv: VIBE_CLIPBOARD_URL   # sandbox env var set to http://localhost:<host port>
```

The host port behind each `publish` entry is fixed in `urlEnv` when the sandbox is created, but a
mapping can change afterwards (`sbx ports --publish`). So at every session start vibe also asks
`sbx ports` for the live mappings and writes each entry's URL to
`/home/agent/.local/state/vibe/published/<name>` inside the sandbox, for helpers there to read in
preference to the variable. If sbx can't confirm a port, the file says so on a second
`unverified:` line.

`publish` entries append across base → profile, so a profile only needs to list what it's adding.
`defaultFeatures` is the exception that replaces rather than appends, and it is read from the base
settings only — it is what the guided picker starts with selected, so it is consulted before the
profile it is creating exists. A feature the sandbox's agent can't use (see `# vibe: agents:`
below) is left out, with a note saying so. Naming a feature the library doesn't have is an error, with the
closest real name suggested:

```
vibe: defaultFeatures in ~/.vibe/settings.yaml: no library feature 'dotnte' — did you mean 'dotnet'?
```

## Library features

`library/<feature>/` holds the building blocks a **guided profile** is composed from: when vibe
offers to create a profile for a folder it hasn't seen, one of the options is to pick features
from this list and order them. A feature is shaped like a small profile, and every part is
optional:

```
library/
  redis/
    config.yaml           # kit fragment — ports, permissions, environment
    settings.yaml          # settings fragment — publish, memory, ...
    install-scripts/       # setup steps, renumbered into the profile's sequence
    agent-files/           # files deployed into /home/agent
    agent-instructions.md  # appended to the profile's agent instructions
```

A feature that only works with some agents says so beside its description, in the same file:
`# vibe: agents: claude` (a comma-separated list; none means every agent). Guided creation only
offers it for a sandbox running one of those agents, and re-composing only offers it to such a
sandbox, or to a profile that has it already, so that taking it out stays your choice. A profile
is shared by every sandbox for its folder, so it may still reach a sandbox running another agent:
vibe warns when it does, and installs it anyway.

Composing writes all of that into the new profile: install scripts are renumbered so the features
run in the order you picked, `agent-files/.local/bin/*` scripts get an `on-start` generated to run
them, and the `config.yaml` / `settings.yaml` fragments are merged into the profile's own, and the
`agent-instructions.md` ones into its `kit-instructions.md` (lists append in feature order; anything the profile itself sets wins). A
feature therefore carries the ports, permissions and instructions its tooling needs — the
`clipboard-bridge` base feature is what gives a sandbox the clipboard page, its published port and
`$VIBE_CLIPBOARD_URL`, and the agent instructions for using it.

A startup script is also how a feature keeps itself current: install scripts run once, when the
sandbox is created, so anything that should be refreshed on every sandbox start belongs in a script
in `agent-files/.local/bin/`, which the generated `on-start` runs (as root: re-enter as the agent
user for anything written under `/home/agent`, or the agent can't replace it later).

A feature vibe no longer has — `diffity`, whose reviews are on the companion page's Review tab now — is
left out, with a note, wherever it's still named: in `defaultFeatures`, and in a profile's recorded
features when it's re-composed or updated.

`clipboard-bridge` lives in `library/base/`: a **base feature**, which every guided profile is
composed with and which is never offered as a choice (a `defaultFeatures` entry, or a recorded
`# vibe: features:` entry, that names it is dropped silently). A profile composed before that, or
by hand, has none of it, and says so when a sandbox is created from it: `vibe --re-compose` rebuilds
a guided profile with it (and recreates the sandbox). It is for sharing images (and text) with the agent. Pasting an
image into the agent's terminal goes through sbx's clipboard bridge, which comes back empty on
Linux hosts, so the feature gives you a page in your browser instead (the agent gives you its URL
via `clipboard-url`, or open `$VIBE_CLIPBOARD_URL`). Paste on it, drop files on it, or pick them,
and each item reaches the agent with your next message (not part-way through a turn, so there's time to
give it a title): a Claude Code hook (`UserPromptSubmit`, in
`/etc/claude-code/managed-settings.d/`) hands over anything not yet delivered, with short text
inline and images as paths for the agent to read. Every item on the page shows whether it has
been sent yet, and has **Copy** (to put it back on your own clipboard) and **Delete** buttons; **Clear all** empties the
history at once (asking first).
The item you pasted or copied last is also what Ctrl-V in the agent's terminal attaches: at every
start, `start-clipboard` puts a fallback in front of sbx's `sbx-clipboard` shim that serves that
item whenever sbx's answer is empty. The history keeps the newest 25 items (and no more than 250 MB of
them), in `~/.vibe/clipboard/<sandbox>/` on your machine: vibe mounts that into the sandbox, where it
shows as `~/.local/state/vibe/clipboard/`, so what you pasted survives the sandbox being rebuilt (`-r`,
`-C`). `--delete` and `--cleanup` remove it with the sandbox. A sandbox made before this keeps its
history inside itself until it is next rebuilt. `start-clipboard` waits for the server to say it's
listening: a server that crashes is reported at once, and one that hasn't started within 30 seconds is
reported as stuck. Either way the companion's clipboard pane shows why. The server logs to
`/tmp/clipboard.log` and `/tmp/clipboard-server.log`.

The profile that comes out is an ordinary profile directory: edit it afterwards like any other.
Composing happens **once**, when the profile is created — the library is never read again, so a
profile carries the version of its features that existed the day it was generated. A generated
profile records what it was made of in its `config.yaml`:

```yaml
# vibe: features: dotnet, redis
```

which is what `vibe -C/--re-compose` works from: it rebuilds the profile from those features as
they are *now* and re-inits the sandbox, so a feature that has since gained an egress rule, a
published port or an instruction reaches the profiles built from it. The profile is regenerated
rather than merged into — re-running the composition over the existing files would append every
fragment a second time, duplicating permissions and published ports — so hand-edits to it are
lost, and it warns of that up front, in yellow, before anything else (enter accepts; `-f` skips the question). The profile is zipped into `~/.vibe/backup/<sandbox>-<date>_<time>.zip` first (the last three per sandbox are kept), and `vibe --restore [path]` lists them, rolls the profile back to the one you pick (after confirming) and offers to re-compose from it; `-f` restores the newest without asking. Before rebuilding it shows the same feature
checklist as guided creation, with the profile's current features selected, so you can add a feature or
drop one you no longer use (and reorder them); with `-f` the recorded features are kept as they are. Profiles written by hand, copied or
created blank record nothing and are refused rather than guessed at; add the comment line above
to adopt one.

Overriding one bundled feature (a base one too, as `~/.vibe/library/base/<feature>/`) means dropping your own `~/.vibe/library/<feature>/` next to it —
unlike `defaults/`, that replaces only the feature you copied, so features the bundle adds later
still show up.

## State

Alongside the configuration, `~/.vibe` (override with `$VIBE_HOME`) is where `vibe` keeps its own
state:

- `instances/<name>.yaml` — which profile and target folder created a sandbox, its timezone, and
  the host port each of its published container ports was given. It is the single source of truth for both:
  `--re-init` reads the profile back so it doesn't need `--profile` repeated, and re-claims the
  recorded host ports so a sandbox's URL stays stable across a rebuild.
- `companion/<name>.token` — the secret token for a sandbox's companion page (readable only by you),
  removed with the sandbox. Its port is in the instance record, as `companionPort`.
- `git/sandboxes/<name>.token` — the GitHub token a sandbox's agent pushes with, if you gave it one
  (readable only by you), kept so a rebuild can hand it on, and removed with the sandbox. The instance
  record says where it came from (`gitToken: default` or `own`), when it expires (`gitTokenExpires`)
  and the rules the agent was given (`gitRules`). `git/default.token` is the default one `--setup`
  stored, with its expiry beside it in `git/default.token.expires`.
- `memories/<name>/` — an agent's backed-up memories across a `--re-init`, when the profile's
  agent supports it. They can only be read out of a *running* sandbox, so once you have agreed to
  remove it, vibe starts a stopped one just long enough to ask whether it holds any before it goes.
  They are also copied back out each time a session ends, so the host copy keeps up. Ctrl-C
  during that copy asks before quitting. Answering yes puts back the memories the store held
  before the copy started, so an interrupted copy never leaves a mix of old and new ones.
- `running/<pid>.lock` — one per vibe process with a folder open (anything but `--ssh`, `--stop`,
  `--stop-all`, `--list`, `--install` and `--cleanup`), locked for as long as that process lives. A second vibe
  for the same folder warns that one is already running, gives its PID, and offers to exit,
  continue anyway, or stop the other one first. `-f` continues anyway without asking.
- `running/<pid>-<n>.lock` — a vibe holding another folder for a while, besides its own: rebuilding
  that folder's sandbox from the Profiles page, or with `--update-sandboxes`. The file says what it's
  doing, so a vibe started in that folder says so too ("vibe (PID n) is rebuilding sandbox 'x'"),
  suggests waiting for it, and warns that stopping it ends the rebuild part-way, along with that
  vibe's own session.

A record outlives the sandbox it describes — `--re-init` tears the sandbox down but keeps the
record, since the ports it remembers are what the rebuild re-claims.

Allocating a host port consults every instance record, not just the running sandboxes: a port
another record claims is skipped even when nothing is listening on it, because that sandbox may
be stopped and will want its port back on the next start. Within one creation the ports already
handed out are skipped for the same reason — the sandbox that will bind them doesn't exist yet.
Failing that, vibe walks upward from the container port until it finds something free.

These sit next to `config.yaml`, `settings.yaml` and `profiles/`; only the latter three are
configuration, and an overlay holding nothing but state is seeded on the next run.

Earlier versions also kept a `ports/<name>-<container-port>` file holding the same host port as
the instance record. Nothing reads it any more; if your `~/.vibe` has one, it is safe to delete.
