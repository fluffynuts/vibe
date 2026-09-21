## Toolchain

The .NET SDK is installed — run `dotnet --list-sdks` to see which versions.
`dotnet build` and `dotnet test -c IsolatedTesting` work against the workspace.
It is vital to run tests with the build configuration IsolatedTesting, otherwise
tests WILL fail in this environment.

NuGet restore reaches api.nuget.org only — if a restore fails on a private feed,
say so rather than trying to work around it.

## Services

RabbitMQ runs on 5672 as node `rabbit@localhost`, with user `rabbit`
(password `rabbit`) holding full permissions on `/`. Use
`sudo -u rabbitmq env RABBITMQ_NODENAME=rabbit@localhost rabbitmqctl ...`
for any admin commands — without the node name it will not find the broker.
Startup log: `/tmp/rabbitmq.log`; broker log: `/tmp/rabbit@localhost.log`.

Elasticsearch runs on http://localhost:9200, single-node with security
disabled, cluster `yumbi-sandbox`. Yumbi detects it and skips spawning a
TempDb.Elasticsearch container. Startup log: `/tmp/elasticsearch.log`;
server log: `/var/log/elasticsearch/yumbi-sandbox.log`.

If any of these are not answering, run
`/home/agent/.local/bin/on-start` — it is re-entrant and safe to repeat.

mysqld and redis-server are installed for TempDb to spawn; do not start
them yourself.

## Diffity

`diffity` is installed and its skills are available, but no session is
running at startup. Comments are anchored to a specific diff, so a session
started before a commit goes stale as soon as one is made — start a fresh
session when review is wanted.

- `/diffity-diff <ref>` starts a session and opens the diff viewer.
- `/diffity-review <ref>` leaves your own inline comments, tagged
  `[must-fix]`, `[suggestion]`, `[nit]` or `[question]`.
- `/diffity-resolve` reads all open comments — the user's or your own —
  and applies the requested changes.
- `diffity agent list --status open --json` reads the current session's
  comments directly.

There is no browser in this sandbox: always pass `--no-open`, and let the
viewer bind its default port 5391, which is the only container port published
to the host.

5391 is not the port the user reaches it on. It is published to a host port
allocated when the sandbox was created, which differs between sandboxes — so
never quote 5391 to the user, and never guess the URL.

End every command that produces something to look at — `/diffity-diff`,
`/diffity-review`, `/diffity-tree`, `/diffity-tour` — by running:

    diffity-url <ref>

and giving the user exactly what it prints, e.g. after reviewing against
master: `http://localhost:5396?ref=master`. Pass the same ref the command
used, and no argument when it had none. The script reads `VIBE_DIFFITY_URL`,
which is where the real host port is; it warns on stderr when that variable
is missing, and a URL printed with that warning should not be handed over as
if it were right.

If the user says they left comments but `diffity agent list` shows none,
the session's ref no longer matches the diff they commented on (usually
because of an intervening commit). Say so rather than asking them to
re-run `/diffity-review`, which only adds your own comments.
