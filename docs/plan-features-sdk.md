# A features SDK, or better, guided creation of a feature (planned, not built)

Raised 2026-11-05, so it isn't forgotten. A basic note, with the questions left open for when this is picked up.

The want: writing a feature should be something vibe helps with, not something learnt by reading `readme.md` and the
bundled features. Two uses:

1. **Features of your own, on your own machine.** A project needs a database, a tool or a service the bundled features
   don't have.
2. **Trusted features from elsewhere.** The case in mind is two features whose logic lives in private repositories in the
   user's organisation: trusted because the repository is the organisation's own, and wanted without copying files by
   hand into `~/.vibe/library`. Arbitrary features from the internet are a different thing, and not the want (see the
   trust questions below).

Guided creation (vibe asking what the feature is for and writing it) is preferred over a bare SDK (documentation and a
template): it can carry the lessons below into every feature written, where documentation is only read by whoever looks.

## What there is

- A feature is a folder under `library/` (the bundle's) or `~/.vibe/library/` (the user's), found by name from both
  (`Layout.featureNames`), with any of: `install-scripts/`, `agent-files/` (`.local/bin` scripts are started by the
  generated `on-start` unless marked `# vibe: startup: false`), `config.yaml` (ports, permissions, environment, merged
  into the profile), `settings.yaml`, and `agent-instructions.md`. Scripts carry `# vibe: ...` directives (description,
  user, startup, onlyIfMissing, mode...); the first install script's description is what the feature checklist shows.
- So a user can already write a feature by hand, and a copy in `~/.vibe/library/<name>` of a bundled one overrides it
  whole (vibe says so when it is composed). What's missing is any help doing it, and any way to get one from elsewhere.
- Base features (`library/base/`) are composed into every guided profile; the rest are picked.

## What a new feature has to get right (what this session learnt the hard way)

These are what guided creation, and a check on a finished feature, should encode:

- **Every step is non-fatal and says what it did** in `/tmp/sbx-setup.log`, naming what failed, and ends in `true`.
- **Check what it uses, install what is missing** (`command -v`), rather than assume the image has it: the image changed
  under every feature once already (Ubuntu to Debian, Node gone, `xz` and `libnuma` missing, package names differing).
  Apt package names are the image's (`tools/check-apt-packages.sh` checks them, inside a sandbox).
- **No braced shell expansions** (`${...}`) anywhere in a file the kit carries: sbx validates them against its own
  placeholders and refuses the kit.
- **Network hosts** named in a script's URLs are allowed automatically; hosts a download redirects to are not, and go in
  `config.yaml`.
- **A start script is run more than once and by more than one user**: at boot and at each session start (so safe to
  repeat, and now serialised by the generated `on-start`), and by the agent by hand (so its log and lock files must be
  openable by the agent, and it must never destroy what a second run might find half-made).
- **Run as the right user**; a service's data belongs to its own user, and a script run as the agent can't read it.
- **Agent instructions** say what is installed, how to reach it, and what to do when it isn't there.
- **Tests**: install and start steps are shell scripts that can be run against stand-ins (see `src/vibe/*feature_test.go`
  for the pattern), and a real run in a scratch sandbox before a feature is trusted.
- **Don't kill processes by name** in any script, test or tool: the sandbox's own PID 1 and 2 belong to the agent user.

## Proposed (a first shape, to be argued with)

1. **`vibe --new-feature <name>`**, guided: asks what the feature gives the sandbox (a package from apt, a download, a
   service that should be running, a port, environment variables, hosts to reach, instructions for the agent) and writes
   the folder into `~/.vibe/library/<name>/` with scripts that already follow the list above, a test file following the
   pattern vibe's own features use, and the instructions file. Nothing is run.
2. **`vibe --check-feature <name>`**: a linter for a finished feature, mechanical where it can be (braced expansions, a
   shebang, a description directive, a non-fatal ending, package names against the image's apt when run in a sandbox,
   hosts in URLs) and a checklist for what it can't.
3. **Adding one from elsewhere** (`vibe --add-feature <git url>@<ref>`): fetches on the host into
   `~/.vibe/library/<name>/` and records where from and which commit; sandbox builds read only what is already local.

## Open questions (for when this is picked up)

Trust, the part that matters most: a feature's install scripts run as root in the sandbox, its agent instructions go to
the agent (an instruction is a way of steering it), its agent-files can include hooks, and its `config.yaml` can open the
network.

1. **Who is trusted, and how is that said?** A list of trusted sources (an organisation's repositories) in
   `settings.yaml`, shown and confirmed when added? Per feature, or per source?
2. **Review before use.** On adding, and on every update, show what the feature would do (its scripts, the hosts it opens,
   its instructions, its hooks) and what changed since the pinned commit, and ask. Is that enough, or should unreviewed
   updates be impossible rather than discouraged?
3. **Fetching.** On the host, by vibe, never at sandbox build time (so a build is reproducible and offline, and a feature's
   code never arrives unreviewed)? With the user's own git credentials for private repositories (never copied into a
   sandbox)? `git` only, or a release archive too?
4. **Pinning.** By commit hash (a tag can move)? Where is the record kept (`~/.vibe/features.yaml`?), and how does
   `vibe --upgrade` / `--install`'s three-way merge of bundled files (`~/.vibe/package-files`) treat a fetched feature, which
   isn't part of the bundle?
5. **Integrity.** Signed commits or tags required for a trusted source? Worth it for an organisation's private
   repositories, or only for anything public?
6. **Names.** A fetched feature's name against a bundled one (`~/.vibe/library` already overrides whole): refuse, rename, or
   say? And against the versioned-name convention (`mysql-8.4`)?
7. **What a feature may not do.** Should the loader refuse some things outright (writing outside `~/.local`, `/opt` and its
   own folders; reaching `**`; changing `settings.json` deny rules; touching the clipboard or review base features)?
8. **Agents.** Features can already say which agents they work with; does a fetched one need to say so, since an instruction
   written for Claude means nothing to another agent?
9. **Updates and removal.** Does removing a feature remove what it installed in existing sandboxes (no: a rebuild does), and
   is the user told which profiles use it?
10. **Declared packages.** A `requires: apt:` list in `config.yaml` was considered and set aside for the bundled features
    (the gain was small, and it can't express fall-backs, repositories or order). For features written by others a
    declaration is easier to check than a script: worth another look here, together with whether sbx's own kit spec has a
    native way to say it.

## Related

- `docs/plan-agent-helper-in-go.md`: Node becomes a feature a user picks, an obvious first feature to build with this.
- `docs/plan-name-collision.md`, `docs/plan-downgrade.md`: other plans that touch what vibe keeps under `~/.vibe`.
