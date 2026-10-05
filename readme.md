# vibe

Create, start and attach to [Docker Sandboxes](https://github.com/docker/sbx-releases) for
contained agentic coding — driven by **profiles**, so different teams/repos can bring their
own tooling (a .NET + Elasticsearch profile, a Rails profile, whatever a repo needs) without
touching the tool itself. 

It runs on Linux, macOS and Windows — wherever `sbx` does.

Whilst the initial implementation is focused around using Claude within the container,
in theory, any agent supported by sbx can be used.

This software is in alpha state. It works reasonably well for me, but I make no guarantees.

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

```powershell
irm https://raw.githubusercontent.com/fluffynuts/vibe/master/install.ps1 | iex
```

Either one downloads the latest release for your machine, checks it against the release's
checksums, and runs `vibe --install` from it. That puts the `vibe` binary in `~/.local/bin` and
its configuration in `~/.vibe`. On Windows it adds `~\.local\bin` to your user `PATH` if it isn't
there already (open a new terminal to pick it up); on Linux and macOS, where that depends on your
shell's profile, it tells you what to add.
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
what it found with everything ticked, for you to untick what you'd rather leave; enter starts the
upgrades straight away, with no confirmation step. When there's
nothing newer, it says "vibe and sbx are up to date". Unticking everything stops with "nothing
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
up to half your machine's memory. It asks which agent sandboxes run, from the agents `sbx run
--help` lists, and which library features a guided profile starts with ticked. Each question
starts on the package's choice. With `-f`, or no terminal, it keeps the package's choices and only
lowers the memory, if it's more than your machine can give. Every later `--install` makes the same
memory check on your `settings.yaml`, and changes nothing else in it.

`vibe --install`, run from a release's folder (which is all the install scripts and `--upgrade`
do), goes through every file the package ships
into `~/.vibe` (`config.yaml`, `settings.yaml`, `defaults/`, `profiles/`, `library/`) and keeps
your edits:

- **New in this release:** copied, and listed.
- **You never edited it:** updated to the new version without asking.
- **Only you changed it:** kept as it is.
- **You deleted it:** stays deleted.
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
vibe -s/--stop [path]      # stop the sandbox for path (default: $PWD)
vibe -c/--ssh [path]       # ssh into the sandbox for path
vibe -r/--re-init [path]   # remove and recreate the sandbox from its profile
vibe -r -f/--force         # ...without prompting
vibe -f                    # ...and, for an unknown profile, create a blank one
                           #    instead of asking
vibe -R/--re-create [path] # delete the profile too, then re-init — the profile
                           #    is gone, so this always re-prompts
vibe -l/--list             # list every known sandbox and its status
vibe -a/--info [path]      # show the sandbox's settings and, while it runs,
                           #    its memory and disk use
vibe -x/--cleanup          # pick sandboxes from a checklist and delete them;
                           #    the profiles they were built from are kept
vibe -d/--delete [path]    # pick whether to delete the sandbox for path, its
                           #    profile, or both (-f: both, without asking)
vibe -i/--install          # copy the bundle into ~/.vibe and the binary into
                           #    ~/.local/bin; from a newer release, upgrade
vibe -i -u/--update-strategy merge,keep
                           # ...settling files changed on both sides without
                           #    asking: keep, update, merge,keep, merge,update
vibe -U/--upgrade          # check for newer vibe and sbx releases, pick which
                           #    to install (-f: all of them, without asking)
vibe -I/--install-sbx      # install the latest stable Docker Sandboxes (sbx);
                           #    on macOS and Windows, asking how (-f: the first
                           #    way, even when up to date)
vibe -v/--version          # print the version, commit and build date
```

Every option has a long and a short form. `-s`, `-c`, `-a`, `-r`, `-R`, `-C` and `-d` resolve the sandbox
name exactly as a normal run would, so `vibe -s && vibe` restarts whatever you were working on.
`-l` and `-x` work on every sandbox at once and take no path.

`-x`/`--cleanup` is for the sandboxes a machine accumulates. It lists everything `sbx` knows
about — each with its status and, where vibe has a record of it, the profile and folder it was
built for — as a checkbox list; space checks, enter is done. What you checked is then listed back
to confirm (see [Checkbox prompts](#checkbox-prompts)), and that is the only question asked —
you came here to delete sandboxes and have just read back which ones. Sandboxes vibe has no
record of are offered too, since those are the ones most likely to be forgotten. There is no
unattended form of this: `-x` always asks, and `-f` has nothing to skip.

**Profiles are never deleted by `-x`.** A profile outlives the sandboxes built from it, so
cleaning up a sandbox leaves the next `vibe` in that folder able to rebuild it unchanged. Deleting
a profile is the job of `-d`/`--delete` or `-R`/`--re-create`. What a cleanup does discard is vibe's *instance record* for
the sandbox — it has to, because a record claims its published host ports whether or not anything
is listening, so one left behind would reserve those ports against every sandbox made afterwards.

`-a`/`--info` shows the sandbox's profile, agent, memory setting and, for a guided profile, the
library features it was composed from. While the sandbox is running, it also shows the memory and
disk the sandbox is using, read from inside it:

```
sandbox   vibe
folder    ~/code/opensource/vibe
profile   vibe
agent     claude
memory    12g
features  go, node
status    running
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

The sandbox starts ticked, because the next `vibe` in that folder rebuilds it. The profile starts
unticked, because it can hold work that's harder to redo. If other sandboxes use the profile, its
line names them. Only things that exist are offered. A profile that only ships with vibe has no
copy in `~/.vibe` to delete, so it isn't offered. `-f` deletes both without asking. With no
terminal and no `-f`, nothing is deleted. vibe logs what it removed and what it kept. As with
`-x`, removing the sandbox drops vibe's instance record for it too.

`-c`/`--ssh` requires `sbx setup ssh` to have been run once on this machine.

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
❯ copy an existing profile
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

Every sandbox ships vibe's Claude Code colour themes, Amber, Blue, Cyan, Green, Orange, Red and
Yellow, from `defaults/agent-files/.claude/themes/`. They keep Claude's reply text white and
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
  red
  yellow
```

The answer is recorded in the profile's folder as `claude-theme`. A rebuild (`-r`, `-R` or `-C`)
doesn't ask again. It reads the theme the old sandbox's agent is actually set to, including any
change made with `/theme` since, before removing it. If it can't, it uses what the profile
recorded, and it asks only when neither is known. `-f`, or running without a terminal, takes that
answer, or Claude's default, without asking.

### Checkbox prompts

Anywhere vibe asks you to check several things — the guided feature picker, `-x`/`--cleanup`, `-d`/`--delete` —
enter doesn't answer straight away (`--upgrade`'s list is the exception: it just goes ahead). What you checked is listed back, one item per line, and the
list is still there to go back to:

```
Confirm selection:
  - diffity: review viewer
  - dotnet: the .NET SDK

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
profiles/
  <profile-name>/
    config.yaml          # profile-specific kit fragment, merged over the base one
    settings.yaml         # profile-specific settings, merged over the base ones
    install-scripts/      # profile-specific setup steps, run after defaults'
    agent-files/           # profile-specific files, deployed after defaults'
    agent-instructions.md # appended to the base kit's agent instructions
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
- `agentInstructions.content`: the base kit's content, with the profile's `agent-instructions.md`
  appended.

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
  helper the agent or the user runs on demand — like diffity's `diffity-url` — with this.

For a file whose format can't carry a `#` comment (JSON, binary, ...), put the same `key: value`
lines in a sidecar file named `<file>.vibe` next to it instead — e.g. `settings.json.vibe`. The
sidecar itself is never deployed.

Directive lines (inline or sidecar) are stripped from the deployed content, so they never leak
into a real script or config file.

**`settings.yaml`** merges these fields (all optional):

```yaml
memory: 12g          # sandbox memory limit; a new sandbox asks, starting here, capped at
                     #    half the host's memory
agent: claude         # which coding agent to run
nugetDir: ~/.nuget    # mounted into the sandbox and symlinked in, if it exists on the host
memoryRoot: ~/.vibe/memories   # where per-sandbox agent memories are backed up
defaultFeatures:      # library features a guided profile starts with ticked
  - diffity
env:                  # extra environment variables passed to `sbx create -e`
  SOME_VAR: value
publish:              # ports to publish, and (optionally) a stable URL env var for each
  - name: diffity
    ports: [5391]
    urlEnv: VIBE_DIFFITY_URL   # sandbox env var set to http://localhost:<host port>
```

The host port behind each `publish` entry is fixed in `urlEnv` when the sandbox is created, but a
mapping can change afterwards (`sbx ports --publish`). So at every session start vibe also asks
`sbx ports` for the live mappings and writes each entry's URL to
`/home/agent/.local/state/vibe/published/<name>` inside the sandbox, for helpers there to read in
preference to the variable. If sbx can't confirm a port, the file says so on a second
`unverified:` line.

`publish` entries append across base → profile, so a profile only needs to list what it's adding.
`defaultFeatures` is the exception that replaces rather than appends, and it is read from the base
settings only — it is what the guided picker starts with ticked, so it is consulted before the
profile it is creating exists. Naming a feature the library doesn't have is an error, with the
closest real name suggested:

```
vibe: defaultFeatures in ~/.vibe/settings.yaml: no library feature 'diffty' — did you mean 'diffity'?
```

## Library features

`library/<feature>/` holds the building blocks a **guided profile** is composed from: when vibe
offers to create a profile for a folder it hasn't seen, one of the options is to pick features
from this list and order them. A feature is shaped like a small profile, and every part is
optional:

```
library/
  diffity/
    config.yaml           # kit fragment — ports, permissions, environment
    settings.yaml          # settings fragment — publish, memory, ...
    install-scripts/       # setup steps, renumbered into the profile's sequence
    agent-files/           # files deployed into /home/agent
    agent-instructions.md  # appended to the profile's agent instructions
```

Composing writes all of that into the new profile: install scripts are renumbered so the features
run in the order you picked, `agent-files/.local/bin/*` scripts get an `on-start` generated to run
them, and the `config.yaml` / `settings.yaml` / `agent-instructions.md` fragments are merged into
the profile's own (lists append in feature order; anything the profile itself sets wins). A
feature therefore carries the ports, permissions and instructions its tooling needs — picking
`diffity` is what gives a sandbox the review viewer, its published port and `$VIBE_DIFFITY_URL`,
the `registry.npmjs.org` egress rule and the agent instructions for using it.

It also makes sure the agent gives you the review URL, and that it's the right one. The diffity
skills tell the agent the viewer is "running at http://localhost:5391", which is the sandbox's
side of the port mapping, or just to "check your browser". So `diffity` registers two Claude Code
hooks, in `/etc/claude-code/managed-settings.d/` so that your own `~/.claude/settings.json` is
left alone. The first time a turn touches diffity, the agent is told the URL your browser can
open, which `diffity-url` reads from the live-checked `published/diffity` file. When the agent
tries to finish a turn that used diffity while a viewer is running, a reply that lacks that URL,
or quotes another localhost port, is sent back to be fixed. This happens once per turn, so it
can't loop. A profile composed before this existed picks it up with `vibe -C`.

A startup script is also how a feature keeps itself current: install scripts run once, when the
sandbox is created, so `diffity` ships an `update-diffity` and an `update-diffity-skills` that the
generated `on-start` runs. Every sandbox start pulls the latest CLI from npm and re-adds the
matching agent skills — the two are released together — non-fatally, logging to
`/tmp/update-diffity.log` and `/tmp/update-diffity-skills.log`. The skills half re-enters as the
agent user, since `on-start` runs as root and nothing root writes under `/home/agent` can be
replaced by the agent later.

The profile that comes out is an ordinary profile directory: edit it afterwards like any other.
Composing happens **once**, when the profile is created — the library is never read again, so a
profile carries the version of its features that existed the day it was generated. A generated
profile records what it was made of in its `config.yaml`:

```yaml
# vibe: features: diffity, dotnet
```

which is what `vibe -C/--re-compose` works from: it rebuilds the profile from those features as
they are *now* and re-inits the sandbox, so a feature that has since gained an egress rule, a
published port or an instruction reaches the profiles built from it. The profile is regenerated
rather than merged into — re-running the composition over the existing files would append every
fragment a second time, duplicating permissions and published ports — so hand-edits to it are
lost, and it asks before doing that (`-f` answers yes). Profiles written by hand, copied or
created blank record nothing and are refused rather than guessed at; add the comment line above
to adopt one.
Overriding one bundled feature means dropping your own `~/.vibe/library/<feature>/` next to it —
unlike `defaults/`, that replaces only the feature you copied, so features the bundle adds later
still show up.

## The bundled template profile

`profiles/template_dotnet-mysql-rabbitmq-elasticsearch-redis/` is a real example: it installs the
.NET SDK, MySQL/Redis/RabbitMQ and Elasticsearch, and picks up the diffity review tooling the same
way a guided profile does. Copy it as a starting point for a new profile.

## State

Alongside the configuration, `~/.vibe` (override with `$VIBE_HOME`) is where `vibe` keeps its own
state:

- `instances/<name>.yaml` — which profile and target folder created a sandbox, and the host port
  each of its published container ports was given. It is the single source of truth for both:
  `--re-init` reads the profile back so it doesn't need `--profile` repeated, and re-claims the
  recorded host ports so a sandbox's URL stays stable across a rebuild.
- `memories/<name>/` — an agent's backed-up memories across a `--re-init`, when the profile's
  agent supports it. They can only be read out of a *running* sandbox, so once you have agreed to
  remove it, vibe starts a stopped one just long enough to ask whether it holds any before it goes.
  They are also copied back out each time a session ends, so the host copy keeps up. Ctrl-C
  during that copy asks before quitting. Answering yes puts back the memories the store held
  before the copy started, so an interrupted copy never leaves a mix of old and new ones.
- `running/<pid>.lock` — one per vibe process with a folder open (anything but `--ssh`, `--stop`,
  `--list`, `--install` and `--cleanup`), locked for as long as that process lives. A second vibe
  for the same folder warns that one is already running, gives its PID, and offers to exit,
  continue anyway, or stop the other one first. `-f` continues anyway without asking.

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
