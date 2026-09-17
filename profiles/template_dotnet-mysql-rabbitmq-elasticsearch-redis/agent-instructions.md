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
