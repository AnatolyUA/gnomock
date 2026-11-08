# Upgrade notes: Go 1.26, new drivers, new preset defaults

This branch updates the Go toolchain, every Go dependency, every preset's default
image and test matrix, and CI. Most callers only notice newer servers. The items
below are the ones that can change behavior or break a build.

## Breaking changes

1. **Go 1.26 or newer is required** (`go 1.26.0` in `go.mod`). Go 1.24 and 1.25
   are EOL, and the latest dependencies need 1.26. CI tests Go 1.26 and 1.27.
2. **Every preset default moved to a newer version** (table below). Code that
   doesn't call `WithVersion` now starts newer servers. Pin a version with
   `WithVersion` to keep the old behavior.
3. **Old servers.** The Mongo driver (now `mongo-driver/v2`) needs MongoDB 4.4 or
   newer. EOL server versions are no longer tested for any preset.
4. **Init and healthcheck honor the start timeout.** The SQL presets (postgres,
   mysql, mariadb, mssql, cockroachdb) run their healthcheck and init queries
   with the context gnomock passes in. An init that runs past `WithTimeout`
   (for example a large `WithQueriesFile` migration) now fails with a context
   error instead of finishing late.
5. **`WithOptions` and gnomockd honor every documented field.** `privileged`,
   `cmd`, `entrypoint`, `disable_cleanup`, `use_local_images_first`, `auth`,
   `host_mounts`, `extraHosts`, `reuse`, `customImage` and `user` used to be
   silently ignored. They are now applied, also for Go callers that pass a
   populated `Options` to `WithOptions`. Unset fields don't override options
   applied earlier. See the security section in `docs/server.md`: an exposed
   gnomockd can now start privileged containers with host mounts.
6. **Pull failures fail at pull time.** An unknown tag or a denied pull now
   fails with `can't pull image: ...` and the daemon's message, instead of a
   later `can't create container: ... No such image`. Code that matched the old
   error text needs updating.
7. **Kafka.** `ZooKeeperPort` is deprecated. With Kafka 4.x (the default) there
   is no ZooKeeper, and the `zookeeper` port is not exposed. It is still exposed
   for versions below 4.
8. **LocalStack.**
   - The default `4.14` is the last image that runs without an auth token and
     will get no updates. 2026.x images need a token: use `WithAuthToken`
     (JSON `auth_token`), or set `LOCALSTACK_AUTH_TOKEN` on the host.
   - The `web` port (8080), served only by LocalStack before 0.11.3, is no
     longer exposed. Images before 1.3.0 are not supported.
9. **MySQL on arm64** uses `library/mysql` instead of `mysql/mysql-server`, the
   same image as amd64. This fixes tags that only `library/mysql` publishes
   (for example `8`), but very old tags (before about 8.0.29) have no arm64 image.
10. **k3s.** Kubernetes 1.35+ (the default) needs a cgroup v2 Docker host. On a
    cgroup v1 host, pass `--kubelet-arg=fail-cgroupv1=false` through
    `K3sServerFlags` (not set by default: kubelets before 1.31 reject the flag).
11. **Splunk.** `WithLicense(true)` also accepts the Splunk General Terms
    (`SPLUNK_GENERAL_TERMS`), which Splunk 10 requires. The default is `10.4`,
    not `latest`.

## Smaller behavior changes

- **Postgres timezone.** `TZ` is passed only when `WithTimezone` is used. Before,
  an empty `TZ=` was always passed, which current images turn into the server
  timezone `UNIVERSAL0`.
- **RabbitMQ.** RabbitMQ 4.3+ refuses transient non-exclusive queues by default.
  The preset allows them again through `RABBITMQ_SERVER_ADDITIONAL_ERL_ARGS`
  (`transient_nonexcl_queues`). If you set that variable yourself, your value
  replaces the preset's.
- **gnomockd router** (`net/http` instead of `gorilla/mux`). Routes, status codes
  for valid requests and response bodies are unchanged. Edge cases:
  - A wrong method still gets 405, now with an `Allow: POST` header, a
    `Method Not Allowed` body and `Content-Type: text/plain`.
  - Unclean paths (`//stop`) redirect with 307 (keeps the POST) instead of 301.
  - A percent-encoded `/` or `.` in the preset name (`/start/a%2Fb`) now gets the
    JSON "preset not found" 404.
- **Debug log redaction.** With `WithDebugMode` (JSON `debug`), the logged
  config no longer shows the registry `auth` string or the values of
  secret-looking environment variables (names containing `PASS`, `TOKEN`,
  `SECRET`, `KEY`, `AUTH`, `CREDENTIAL` or `ACCOUNTS`, such as
  `POSTGRES_PASSWORD` or `LOCALSTACK_AUTH_TOKEN`). The variable names are
  still logged, and the container still gets the real values.
- **InfluxDB** supports 2.x only. The floating `latest`/`alpine` tags may point
  to v3, which isn't supported, so pin a 2.x tag.
- **Architectures.** linux/amd64 and linux/arm64 are tested. mssql and splunk
  are amd64 only.

## New preset defaults

| Preset | Old default | New default | Tested versions |
|---|---|---|---|
| azurite | 3.34.0 | 3.37.0 | 3.37.0 |
| cassandra | 3 | 5.0 | 4.0, 4.1, 5.0 |
| cockroachdb | v25.1.6 | v26.3.5 | v24.3.37, v25.2.27, v25.4.20, v26.2.11, v26.3.5 |
| elastic | 8.13.0 | 9.5.5 | 8.19.23, 9.4.8, 9.5.5 |
| influxdb | 2.7.6-alpine | 2.9-alpine | 2.8-alpine, 2.9-alpine |
| k3s | v1.26.3-k3s1 | v1.37.2-k3s1 | v1.35.10, v1.36.6, v1.37.2 (`-k3s1`) |
| kafka | 3.6.1-L0 | 4.3.1-L0 | 3.9.0-L0, 4.3.1-L0 |
| localstack | 0.14.0 | 4.14 | 3.8, 4.14, 2026.10 (with a token) |
| mariadb | 10.5.8 | 12.3 | 10.11, 11.4, 11.8, 12.3, 13.0 |
| memcached | 1.6.9 | 1.6.45 | 1.6.45 |
| mongo | 5.0 | 9.0 | 7.0, 8.0, 8.3, 9.0 (8.0/8.3 skipped in CI, see below) |
| mssql | 2019-latest | 2025-latest | 2017 (local only), 2019, 2022, 2025 |
| mysql | 8.0.22 | 9.7 | 8.4, 9.7, 26.10 |
| postgres | 16.2 | 18 | 15, 16, 17, 18 |
| rabbitmq | 3.8.9 | 4.4 | 4.3-alpine, 4.4-alpine, 4.4-management-alpine |
| redis | 6.0.9 | 8.10 | 6.2, 7.2, 7.4, 8.2, 8.10 |
| splunk | latest | 10.4 | 10.0, 10.2, 10.4 |
| vault | 1.13.0 | 2.2 | 2.2 |

## Driver and dependency changes (internal, no public API change)

- Redis: `go-redis/redis/v7` → `redis/go-redis/v9`.
- Mongo: `mongo-driver` v1 → `mongo-driver/v2`.
- Cassandra: `gocql/gocql` → `apache/cassandra-gocql-driver/v2`.
- Elastic: the `go-elasticsearch/v7` client and its tests are gone. The preset uses
  `go-elasticsearch/v8`, which also works with 9.x servers.
- `gorilla/mux` and `golang.org/x/mod` are no longer dependencies.

## Known gaps

- **MongoDB 8.0 and 8.3 are skipped in CI.** They refuse to start on Linux 6.19+
  kernels (which the CI runners use) until mongo 8.0.37 / 8.3.16 are published.
  They pass on older kernels. The removal steps are in the
  `TODO(mongo-kernel-guard)` comment in `preset/mongo/preset_test.go`.
- **mssql 2017 and Go's x509 check.** SQL Server 2017 creates a self-signed
  certificate with a random serial number, and it is often negative. Go 1.23+
  rejects negative serials (`x509: negative serial number`) unless the main
  module's `go` line is below 1.23 or `GODEBUG=x509negativeserial=1` is set.
  The healthcheck then never connects, and `Start` fails at the timeout.
  Callers whose own module says `go 1.23` or newer already had this, and the
  Go 1.26 floor now applies it to everyone. 2017 is skipped in CI, so CI
  doesn't catch it. Workaround: run with `GODEBUG=x509negativeserial=1`, or add
  `//go:debug x509negativeserial=1` to the main package or test file. 2019 and
  newer are not affected.
- **azblob is held at v1.7.0.** v1.8.x sends a storage API version that no
  released Azurite (newest 3.37.0) accepts.
- **CodeQL** is disabled on the fork by GitHub (`disabled_fork`) and did not run
  on this branch.
