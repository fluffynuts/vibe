# vibe

Create, start and attach to [Docker Sandboxes](https://github.com/docker/sbx-releases) for
contained agentic coding — driven by **profiles**, so different teams/repos can bring their
own tooling (a .NET + Elasticsearch profile, a Rails profile, whatever a repo needs) without
touching the tool itself.

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
```

Every option has a long and a short form. `-s`, `-c`, `-r`, `-R` and `-l` resolve the sandbox
name exactly as a normal run would, so `vibe -s && vibe` restarts whatever you were working on.

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
memory: 12g          # sandbox memory limit
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
