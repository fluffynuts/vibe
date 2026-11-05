## MariaDB

A MySQL-compatible server is installed: MariaDB, from Debian, which has no Oracle MySQL. `mysqld`, `mysql` and
`mysqladmin` are on the PATH (MariaDB's, under the MySQL names), and `start-mariadb` starts the server when the
sandbox starts. It listens on 127.0.0.1:3306 and on `/var/run/mysqld/mysqld.sock`, and logs to `/tmp/mariadb.log`.

- Administer it with `sudo mysql`: root signs in through the socket, so `mysql -uroot` as yourself is refused.
- Give a project its own user and database, and connect over TCP with them:
  `sudo mysql -e "create database app; create user 'app'@'%' identified by 'secret'; grant all on app.* to 'app'@'%'"`.
- If the server isn't running, run `start-mariadb` and read `/tmp/mariadb.log`. If there is no `mysqld` at all, its
  install failed: the reason is in `/tmp/sbx-setup.log`.
- It is MariaDB, not Oracle MySQL: a project that needs MySQL 8 behaviour (its JSON, authentication or SQL
  differences) may not run on it, and the `mysql-8.4` feature is Oracle's own. Say so rather than installing a
  different server beside this one.
