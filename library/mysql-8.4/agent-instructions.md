## MySQL 8.4

Oracle's MySQL Community Server 8.4 is installed (its own Linux build, not MariaDB): `mysqld`, `mysql`, `mysqladmin`,
`mysqldump` and the rest are on the PATH, from `/opt/mysql-8.4`. `start-mysql-8.4` starts a server when the sandbox
starts, on 127.0.0.1:3306 and on the socket `/run/mysqld/mysqld.sock` (`MYSQL_UNIX_PORT` names it, so `mysql` finds it),
with its data in `/var/lib/mysql-8.4`, and logs to `/tmp/mysql-8.4.log`.

- The server's `root` account has no password, and it can only be reached from this machine: `mysql -uroot` is enough.
  Give a project its own user and database as you would anywhere:
  `mysql -uroot -e "create database app; create user 'app'@'%' identified by 'secret'; grant all on app.* to 'app'@'%'"`.
- A project that starts a `mysqld` of its own (a throwaway test database, say) can: `mysqld` on the PATH finds its own
  files. Run it as yourself, with a data folder of its own and a port other than 3306 (the server above has that), and
  pass `--no-defaults` so no option file meant for another server is read.
- If the server isn't running, run `start-mysql-8.4` and read `/tmp/mysql-8.4.log`. If there is no `mysqld`, its install
  failed: the reason is in `/tmp/sbx-setup.log`.
- Don't install another MySQL or MariaDB beside this one: say what the project needs instead.
