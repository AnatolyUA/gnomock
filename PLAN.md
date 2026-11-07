# Upgrade plan: Go, dependencies, images, CI

> Temporary working document. Delete it once the work is finished.
> Research date: 2026-11-07.

## Goal

Bring this fork up to date: the Go toolchain, every Go dependency, every preset's
default image and test matrix, and CI/tooling. Ship it as one body of work on one
branch. Each step is a separate commit so it can be reviewed or reverted on its own.

- **Repository:** `AnatolyUA/gnomock` (fork). Upstream stays `orlangure/gnomock`.
- **Branch:** `claude/blissful-ptolemy-5pkco6`. It was created from `master` @ `5ce19d1`
  and all contributions go here.

## Decisions (agreed)

| Topic | Decision |
|---|---|
| Module path / images | Keep `github.com/orlangure/gnomock` and the upstream `orlangure/*` image names. CI builds the server and cleaner images locally but never pushes them. |
| Version policy | The test matrix covers every vendor-supported (non-EOL) version line. The default is the newest stable or LTS line. Delete code paths that exist only for EOL versions. Deprecate exported identifiers instead of deleting them. |
| Kafka | Default `4.3.1-L0`. Test `3.9.0-L0` and `4.3.1-L0`. Expose the ZooKeeper port only for versions below 4. Mark `ZooKeeperPort` as `// Deprecated:`. |
| LocalStack | Default `4.14`, the last tag that runs without an auth token. Add `WithAuthToken(token)` for 2026.x; when it isn't set, fall back to the host's `LOCALSTACK_AUTH_TOKEN` env var, matching LocalStack's own tooling. The 2026.x test runs only when that env var is set: CI maps it from the repo secret (already added), and it is skipped otherwise. It can't run in the cloud session because containers there have no internet access to activate the licence. |
| Elasticsearch client | Stay on `go-elasticsearch/v8`, latest 8.19.x. It is forward-compatible with 9.x servers. Drop the v7 client and the 7.17 test. |
| InfluxDB | Support 2.x only, with pinned tags. Document that v3 is not supported. |
| CI extras | Add an arm64 job. Remove CircleCI. Remove Codecov (no token in the fork). |
| Go version | `go 1.26.0` in `go.mod`. Go 1.24 and 1.25 are EOL, and the latest dependencies force at least 1.26. CI tests Go 1.26 and 1.27. |
| Releases | No separate releases. Everything lands on the branch above. |

### Implementation defaults (no input needed, but flag disagreement)

- **Tag style.** Use floating `major.minor` tags where the image publishes them
  (`postgres:18`, `mysql:9.7`), so patch releases arrive automatically. Use exact tags
  where no floating minor tag exists (elasticsearch, k3s, fast-data-dev, azurite,
  cockroachdb, memcached).
- **Router.** Replace `gorilla/mux` with the Go ≥1.22 `net/http` pattern router. That
  drops a dependency, and gnomockd uses only two routes.
- **Modernizers.** Run `go fix` (the Go 1.26 modernizers) as its own commit, separate from
  any behavior change.
- **Redis matrix.** For Redis, "every supported line" is collapsed to the LTS-like lines
  plus the newest minor: 6.2, 7.2, 7.4, 8.2, 8.10.

## Verification strategy

- **Local, amd64.** The session container can run `dockerd`, so every preset's
  integration tests run locally before each push. Images are pruned between presets to
  stay inside the disk allowance (about 24 GB free).
- **arm64.** Covered only by the new arm64 CI job and `GOARCH=arm64 go vet ./...`.
- **CI on the fork.**
  - **Prerequisite (owner):** GitHub Actions must be enabled on the fork
    (Settings → Actions). Forks have it disabled by default.
  - The workflows currently trigger only on `push: master` and `pull_request`. Add a
    `push` trigger for all branches plus `workflow_dispatch` so this branch runs CI
    without a PR.
- **Rule.** Every phase ends green, meaning `go build`, `go vet`, lint, and the
  affected integration tests, before the next phase starts.

---

## Phase 1: Safety-net tests (against the current code, before any upgrade)

Every new test must pass on the current `master` code first. That way it proves the
upgrade didn't change behavior, rather than describing the new behavior.

- [x] **Unit tests for the moby type conversions** (no Docker needed):
  - [x] `exposedPorts` (`docker.go:275`): tcp/udp, out-of-range ports skipped.
  - [x] `portBindings` (`docker.go:294`): custom `HostPort`, the `isInDocker()`
        `0.0.0.0` branch, and the non-IP `DOCKER_HOST` fallback.
  - [x] `boundNamedPorts` (`docker.go:424`): IPv4 plus `::` binding pair, empty binding,
        bad `HostPort`.
  - [x] Gateway extraction in `waitForContainerNetwork` (`docker.go:257-262`).
- [x] **Daemon error matching.**
  - [x] Test the duplicate-name regex (`docker.go:30`).
  - [x] Test `isDeletionAlreadyInProgessError` (`docker.go:519`).
- [x] **Image pull and local-image lookup.**
  - [x] Extract the tag-matching logic from `isExistingLocalImage` (`docker.go:65-88`)
        into a pure function and unit-test it.
  - [x] Integration test: `WithUseLocalImagesFirst` with a locally tagged image.
- [x] **`WithOptions` and the gnomockd JSON contract.**
  - [x] Golden test: decode a `startRequest` JSON containing every documented field,
        apply `WithOptions`, assert the resulting `Options`. Today this test documents
        the bug: `privileged`, `cmd`, `disable_cleanup`, `use_local_images_first` and
        `auth` are dropped. It gets flipped in Phase 4.
  - [x] Golden test of the response JSON for `Container` and for errors
        (`internal/errors`).
  - [x] Validate every `internal/gnomockd/testdata/*.json` against
        `swagger/swagger.yaml`, and check that preset JSON tags match the swagger
        properties.
- [x] **Sidecar cleaner.**
  - [x] Round-trip tests for `generateID`/`parseID` (`container.go:70-89`).
  - [x] Integration test: a sidecar exists after `Start`, and is gone after `Stop`.
- [x] **Per-arch image selection.** Unit-test `Image()` for every preset, including the
      MySQL amd64/arm64 split, so removing that split in Phase 6 is deliberate.
- [x] **Registry.** Test that `cmd/server` registers all 18 presets.
- [x] **Log streaming.** Unit-test `copyf` and `closeLogReader` (`gnomock.go:154-179`)
      using `stdcopy.NewStdWriter`. (moby/api v1.54 has no `NewStdWriter`, so the tests
      build frames by hand from the documented wire format.)

## Phase 2: Tooling and CI

- [ ] **golangci-lint.**
  - [ ] Upgrade v1.64.5 → v2.15.0 and migrate `.golangci.yml` to the v2 format
        (`golangci-lint migrate`). `gosimple` and `stylecheck` merge into `staticcheck`.
        `gofmt`, `gofumpt` and `goimports` move to `formatters`. `wsl` becomes `wsl_v5`.
  - [ ] Fix or justify the new findings.
- [ ] **Actions versions.** `actions/checkout@v7`, `actions/setup-go@v7`,
      `golangci/golangci-lint-action@v9`, `github/codeql-action@v4`,
      `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`,
      `arduino/setup-task@v3`.
- [ ] **Workflow cleanup.**
  - [ ] Replace `go get -v -t -d ./...` with `go mod download`.
  - [ ] Turn the 18 near-identical preset jobs into one matrix job.
- [ ] **Go and triggers.**
  - [ ] Go matrix 1.26 and 1.27 for core jobs. Preset jobs use 1.27.
  - [ ] Add the triggers: push to any branch, and `workflow_dispatch`.
- [ ] **arm64 job.** On `ubuntu-24.04-arm`: core tests plus the postgres, mysql, redis
      and mongo presets. Also add `GOARCH=arm64 go vet ./...` to the lint job.
- [ ] **Service-container job.** Stop using `orlangure/gnomock:v0.20.0`. Build the server
      image in the job instead, and replace the Homebrew/httpie install with `curl`.
- [ ] **Removals.** Delete Codecov (upload steps, `codecov.yml`, README badge) and
      `.circleci/`.
- [ ] **Dockerfiles** (root and `cmd/cleaner`).
  - [ ] Change `golang:latest` to `golang:1.27-alpine`.
  - [ ] Use buildx `TARGETOS`/`TARGETARCH` instead of `ARG GOARCH`.
  - [ ] Add `-trimpath`.
- [ ] **Cleaner module.**
  - [ ] Add a root `go.work` (`.` and `./cmd/cleaner`) so the cleaner builds and tests
        against local code.
  - [ ] Build the cleaner image from the repo-root context.
  - [ ] Add a CI job that builds both images for amd64 and arm64, with no push.
- [ ] **Taskfile.** Update the build tasks to match. Keep the upload and release tasks
      unchanged: they push to upstream `orlangure/*`, which only upstream can use.

## Phase 3: Go 1.26 and same-major dependency bumps

A trial run of this phase already compiled and passed `go vet` with no code changes.

- [ ] `go.mod`: `go 1.26.0`. Drop the stray `toolchain` line in `cmd/cleaner/go.mod`.
- [ ] Bump every direct dependency to its latest version within the current major:
  - **AWS SDK v2:** the root module plus the config, s3, sqs and sns modules.
  - **Kubernetes:** `k8s.io/{api,apimachinery,client-go}` v0.37.2.
  - **Docker:** `moby/moby/{api,client}` v1.56.1 / v0.6.2.
  - **Database and messaging drivers:**
    - SQL drivers: `go-sql-driver/mysql` v1.10, `lib/pq` v1.12, `go-mssqldb` v1.11.
    - Messaging: `amqp091-go` v1.16, `kafka-go` v0.4.51.
    - Other stores: `gomemcache` latest, `go-elasticsearch/v8` v8.19.x,
      `influxdb-client-go/v2` latest.
  - **Cloud APIs:** `vault/api` v1.23, `azblob` v1.8.
  - **Utilities:** `testify` v1.12, `zap` v1.28, `golang.org/x/{mod,sync}`.
- [ ] Do **not** run a blanket `go get -u` on indirect dependencies. That pulls a
      `structured-merge-diff` v7 that breaks apimachinery 0.37 (seen in the trial).
      Let `go mod tidy` resolve the indirects.
- [ ] Replace `gorilla/mux` with `net/http` patterns.
- [ ] Run `go fix ./...` (modernizers) as a separate commit.

## Phase 4: Bug fixes found during research

- [ ] **`WithOptions` (`options.go:114`).** Copy every serializable field:
      `Privileged`, `Cmd`, `Entrypoint`, `DisableAutoCleanup`, `UseLocalImagesFirst`,
      `Auth`, `HostMounts`, `ExtraHosts`, `Reuse`, `CustomImage`, `User`. Then flip the
      Phase 1 golden test, and add the missing fields to `swagger.yaml`.
- [ ] **`pullImage` (`docker.go:90`).** Decode the JSON message stream and surface
      `{"error": ...}` entries instead of discarding them.
- [ ] **Error matching.** Replace daemon error-string matching with `cerrdefs`
      (`IsConflict` and friends) where the API supports it. Keep the Phase 1 tests.

## Phase 5: Major driver upgrades (internal only, no public API change)

- [ ] **Redis.** `go-redis/redis/v7` → `github.com/redis/go-redis/v9`, in the preset and
      its tests. The gnomockd test moves from `go-redis/redis` v6 to v9.
- [ ] **Mongo.** `go.mongodb.org/mongo-driver` v1 → `go.mongodb.org/mongo-driver/v2`.
      The `bsonrw` package is gone in v2, so rework the data-loading code. v2 needs
      server 4.2 or newer, which is fine under the new matrix.
- [ ] **Cassandra.** `github.com/gocql/gocql` → `github.com/apache/cassandra-gocql-driver/v2`.
- [ ] **Elastic.** Remove the `go-elasticsearch/v7` dependency and the v7 tests.

## Phase 6: Presets (images, matrices, defaults)

| Preset | Default: old → new | Test matrix (new) | Extra work |
|---|---|---|---|
| postgres | 16.2 → **18** | 15, 16, 17, 18 | PG 18 changed the image's data-dir layout. Check the init/queries file handling. |
| mysql | 8.0.22 → **9.7** (LTS) | 8.4, 9.7, 26.10 | Delete `preset_arm64.go`/`preset_amd64.go` and use `library/mysql` everywhere. `mysql_native_password` is gone in 9.x, so verify `caching_sha2_password` over plain TCP. |
| mariadb | 10.5.8 → **12.3** (LTS) | 10.11, 11.4, 11.8, 12.3, 13.0 | MariaDB 11+ images have no `mysql*` binaries. Check that the healthcheck/init doesn't call them. |
| redis | 6.0.9 → **8.10** | 6.2, 7.2, 7.4, 8.2, 8.10 | Driver is v9 (Phase 5). |
| mongo | 5.0 → **9.0** | 7.0, 8.0, 8.3, 9.0 | Driver is v2 (Phase 5). |
| memcached | 1.6.9 → **latest 1.6.x** (exact) | that single tag | |
| rabbitmq | 3.8.9 → **4.4** | 4.3-alpine, 4.4-alpine, plus management variant | RabbitMQ 4 removed classic mirrored queues. Check the preset's queue setup. |
| cassandra | 3 → **5.0** | 4.0, 4.1, 5.0 | Driver is the apache module (Phase 5). |
| elastic | 8.13.0 → **9.5.5** | 8.19.23, 9.4.x, 9.5.5 | Client stays v8. |
| influxdb | 2.7.6-alpine → **2.9-alpine** | 2.8-alpine, 2.9-alpine | Replace the floating `alpine` tag. Add a doc note that v3 isn't supported. |
| k3s | v1.26.3-k3s1 → **v1.37.2-k3s1** | v1.35.10, v1.36.6, v1.37.2 (`-k3s1`) | Verify privileged k3s on a cgroup v1 host locally. CI runners are cgroup v2. Drop version branches for old k3s, if any. |
| cockroachdb | v25.1.6 → **v26.3.x** | v24.3, v25.2, v25.4, v26.2, v26.3 (latest patches) | Check any version-dependent flags in the preset. |
| kafka | 3.6.1-L0 → **4.3.1-L0** | 3.9.0-L0, 4.3.1-L0 | ZooKeeper port only for versions below 4. Deprecate `ZooKeeperPort`. Confirm what listens on 2181 in 4.x (the image config still declares it). |
| localstack | 0.14.0 → **4.14** | 3.8, 4.14, plus 2026.10 when a token is present | Add a token option. Remove 0.x-specific code (`0.12.2` is already deleted from Docker Hub). Check the services/healthcheck endpoints in 4.x. |
| vault | 1.13.0 → **2.2** | 2.2 | Review the Vault 2.0 breaking changes that affect the dev server, tokens and policies. |
| mssql | 2019-latest → **2025-latest** | 2017 (skipped in CI, as today), 2019, 2022, 2025 | amd64 only, document it. Check the TLS/encrypt defaults with SQL 2022+. |
| azurite | latest → **3.37.0** | 3.37.0 | Pin instead of `latest`. |
| splunk | latest → **10.4** | 10.0, 10.2, 10.4 | amd64 only, document it. Add the `SPLUNK_GENERAL_TERMS` acceptance env (believed required since 9.4/10). |

For every row:

- [ ] Update the `defaultVersion` constant and the doc comments.
- [ ] Update the `preset_test.go` matrix.
- [ ] Update the gnomockd testdata JSON. It is stale today: kafka 2.5.1, localstack 0.11,
      elastic 8.7, and several `latest` tags.
- [ ] Add a `withDefaults` test where one is missing (azurite, cassandra, redis, splunk).
- [ ] Run the preset and gnomockd tests locally.

## Phase 7: API docs and generated code

- [ ] **`swagger/swagger.yaml`.** Update the default versions, the new `Options` fields
      (Phase 4), the LocalStack token option, and the Kafka ZooKeeper deprecation.
- [ ] **`cmd/generator` and `cmd/server/presets.go`.** Regenerate and confirm that
      nothing else changes.
- [ ] **Docs.** Update `README.md` (preset table and versions, supported architectures,
      Go version), `docs/server.md` and the per-preset readmes.
- [ ] **Architectures section.** Supported: linux/amd64 and linux/arm64. mssql and splunk
      are amd64 only. 32-bit arm, 386, ppc64le and s390x are not tested.

## Phase 8: Final verification and cleanup

- [ ] Run the full local integration suite with every preset.
- [ ] Get CI green on the branch: lint, core, presets, arm64, image builds.
- [ ] Re-run the dependency check (`go list -m -u`) and the image-tag check, in case
      anything was released during the work.
- [ ] Write a "breaking changes" summary (below) into the final report or PR description.
- [ ] Delete `PLAN.md`.

## Behavior changes users will notice

1. **New defaults.** Every preset default moves to a new major version. Anyone not using
   `WithVersion` gets newer servers.
2. **Go floor.** Go 1.26 or newer is required.
3. **MySQL on arm64.** It now uses `library/mysql` instead of `mysql/mysql-server`.
   Very old MySQL tags (before about 8.0.29) have no arm64 image.
4. **Kafka.** `ZooKeeperPort` is deprecated, and with Kafka 4.x there is no ZooKeeper.
5. **LocalStack.** The default 4.14 is a frozen image. 2026.x needs an auth token.
6. **gnomockd / `WithOptions`.** These now honor `privileged`, `cmd`, `auth` and the other
   fields that used to be silently ignored.
7. **Old servers.** The Mongo driver needs server 4.2 or newer, and EOL server versions are
   no longer tested.

## Risks and open verification items

- **Kafka 4.x.** Unclear whether port 2181 is actually served. Decided empirically in
  Phase 6.
- **Splunk.** The `SPLUNK_GENERAL_TERMS` requirement is unconfirmed. Decided empirically.
- **LocalStack 4.x.** Possible healthcheck/endpoint changes.
- **k3s.** Recent versions on this cgroup v1 container host may fail locally. If so, rely
  on CI (cgroup v2).
- **Disk.** Splunk, mssql, elastic and k3s images are large, so prune between runs.
- **cmd/cleaner.** It still `require`s the published gnomock v0.32.0 for anyone building
  outside the workspace. `go.work` fixes local and CI builds only.

## Out of scope

- InfluxDB 3 support.
- Publishing images to a registry.
- Renaming the module path.
- Moving from `lib/pq` to `pgx`.
- Upgrading from the Elastic v8 client to v9.
