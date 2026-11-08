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
        the bug: 11 fields are dropped (the Phase 4 list, including `privileged`, `cmd`,
        `disable_cleanup`, `use_local_images_first` and `auth`). It gets flipped in Phase 4.
  - [x] Golden test of the response JSON for `Container` and for errors
        (`internal/errors`).
  - [x] Validate every `internal/gnomockd/testdata/*.json` against
        `swagger/swagger.yaml` (key names, plus value types for scalar properties), and
        check that preset JSON tags match the swagger properties.
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

- [x] **golangci-lint.**
  - [x] Upgrade v1.64.5 → v2.15.0 and migrate `.golangci.yml` to the v2 format
        (`golangci-lint migrate`). `gosimple` and `stylecheck` merge into `staticcheck`.
        `gofmt`, `gofumpt` and `goimports` move to `formatters`. `wsl` becomes `wsl_v5`.
  - [x] Fix or justify the new findings. `noctx` (11): presets and `internal/health`,
        `internal/cleaner` now pass the context through. `govet inline` (2):
        `reflect.Ptr` → `reflect.Pointer`. `prealloc` (3) and `wsl_v5` (2) fixed.
        `goconst` (36) is justified, not fixed: goconst ≥ 1.8 also counts composite
        literals (test tables, k3s manifests), so `exclude-types: [Call, CompositeLit]`
        keeps the v1 scope.
- [x] **Actions versions.** `actions/checkout@v7`, `actions/setup-go@v7`,
      `golangci/golangci-lint-action@v9`, `github/codeql-action@v4`,
      `docker/setup-qemu-action@v4`, `docker/setup-buildx-action@v4`,
      `arduino/setup-task@v3`.
- [x] **Workflow cleanup.**
  - [x] Replace `go get -v -t -d ./...` with `go mod download`.
  - [x] Turn the 18 near-identical preset jobs into one matrix job.
- [x] **Go and triggers.**
  - [x] Go matrix 1.26 and 1.27 for core jobs. Preset jobs use 1.27.
  - [x] Add the triggers: push to any branch, and `workflow_dispatch`.
- [x] **arm64 job.** On `ubuntu-24.04-arm`: core tests plus the postgres, mysql, redis
      and mongo presets. Also add `GOARCH=arm64 go vet ./...` to the lint job.
- [x] **Service-container job.** Stop using `orlangure/gnomock:v0.20.0`. Build the server
      image in the job instead, and replace the Homebrew/httpie install with `curl`.
      `services:` start before the steps, so the job now `docker run`s the built image
      with the same socket mount and port.
- [x] **Removals.** Delete Codecov (upload steps, `codecov.yml`, README badge) and
      `.circleci/`. `cmd/generator` no longer writes CircleCI jobs and now adds a
      `test-preset` matrix entry instead of a whole job.
- [x] **Dockerfiles** (root and `cmd/cleaner`).
  - [x] Change `golang:latest` to `golang:1.27-alpine`.
  - [x] Use buildx `TARGETOS`/`TARGETARCH` instead of `ARG GOARCH`.
  - [x] Add `-trimpath`.
- [x] **Cleaner module.**
  - [x] Add a root `go.work` (`.` and `./cmd/cleaner`) so the cleaner builds and tests
        against local code.
  - [x] Build the cleaner image from the repo-root context.
  - [x] Add a CI job that builds both images for amd64 and arm64, with no push.
- [x] **Taskfile.** Update the build tasks to match. Keep the upload and release tasks
      unchanged: they push to upstream `orlangure/*`, which only upstream can use. Build
      tasks lost `--build-arg GOARCH`; the cleaner builds from the root context. The
      core test tasks dropped the coverage flags but still run the same tests (both
      gnomockd commands, `./cmd/server` in internal).
- [x] **arm64 follow-ups** (found by the first arm64 CI run; owner chose the fix).
  - [x] `TestGnomock_witUseLocalImagesFirst` used `circleci/mongo:4.4`, which is amd64
        only. It now uses the `library/redis:7.2` / `cimg/redis:7.2` pair. Both are
        published for amd64 and arm64, and they share `name:tag` the same way, so the
        issue #302 check is unchanged.
  - [x] **Known gap:** `mysql/mysql-server:5.7.32` (the image the arm64 mysql preset
        picks for its 5.7 test) is amd64 only. The `5.7.32` case of `preset/mysql`
        `TestPreset` is skipped on arm64 only. Phase 6 removes the skip together with
        5.7 and the per-arch split.
  - [x] **Known gap:** gnomockd `TestMySQL` asks for version `"8"`
        (`internal/gnomockd/testdata/mysql.json`). The arm64 preset
        (`preset/mysql/preset_arm64.go`) maps that to `mysql/mysql-server:8`, which
        doesn't exist, so the request fails. This is a real arm64 bug from the
        per-arch split: any arm64 user asking for a tag that only `library/mysql`
        publishes hits it. The test is skipped on arm64 only. Phase 6 removes the skip
        together with the per-arch split.

## Phase 3: Go 1.26 and same-major dependency bumps

A trial run of this phase already compiled and passed `go vet` with no code changes.

- [x] `go.mod`: `go 1.26.0`. Drop the stray `toolchain` line in `cmd/cleaner/go.mod`.
      `go.work` and `cmd/cleaner/go.mod` also say `go 1.26.0` (a workspace `go` line
      older than a module's breaks the workspace). No `go work sync`: it would raise the
      cleaner's indirect requirements, which stay as they are.
- [x] Bump every direct dependency to its latest version within the current major:
  - [x] **AWS SDK v2:** the root module plus the config, s3, sqs and sns modules.
  - [x] **Kubernetes:** `k8s.io/{api,apimachinery,client-go}` v0.37.2.
  - [x] **Docker:** `moby/moby/{api,client}` v1.56.1 / v0.6.2.
  - [x] **Database and messaging drivers:**
    - SQL drivers: `go-sql-driver/mysql` v1.10, `lib/pq` v1.12, `go-mssqldb` v1.11.
    - Messaging: `amqp091-go` v1.16, `kafka-go` v0.4.51.
    - Other stores: `gomemcache` latest, `go-elasticsearch/v8` v8.19.x,
      `influxdb-client-go/v2` latest.
    - Also bumped within the same major: `mongo-driver` v1.17.10 (Phase 5 moves it to
      v2). `influxdb-client-go/v2`, `gocql`, `go-redis` v6/v7 and `go-elasticsearch/v7`
      were already at their latest same-major versions.
  - [x] **Cloud APIs:** `vault/api` v1.23, `azblob` v1.8.
    - **azblob held at v1.6.1** (owner's decision). No azblob 1.8.x works with any
      released Azurite: 1.8.0+ sends storage API version 2026-06-06 or newer, while
      Azurite 3.34.0 (current default) accepts up to 2025-05-05 and 3.37.0 (newest) up
      to 2026-04-06. With v1.8.3 every azurite test failed with 400 "The API version
      … is not supported by Azurite". v1.6.1 is the newest that works with 3.34.0; v1.7.0
      is the newest that works with 3.37.0. Phase 6 bumps azblob to v1.7.0 together with
      Azurite 3.37.0. 1.8 waits for an Azurite release that supports its API version
      (v1.8.1+ would also pull Apache Arrow into `preset/azurite`).
  - [x] **Utilities:** `testify` v1.12, `zap` v1.28, `golang.org/x/{mod,sync}`, plus
    `go.yaml.in/yaml/v3` v3.0.6. Apart from the azblob hold, `go list -m -u` shows no
    same-major update for any direct dependency.
- [x] Do **not** run a blanket `go get -u` on indirect dependencies. That pulls a
      `structured-merge-diff` v7 that breaks apimachinery 0.37 (seen in the trial).
      Let `go mod tidy` resolve the indirects.
- [x] Replace `gorilla/mux` with `net/http` patterns. Routes and the JSON contract
      tests are unchanged; edge-case differences are listed under behavior changes (9).
- [x] Run `go fix ./...` (modernizers) as a separate commit. It took two passes to
      converge: the first pass leaves `f := f` copies in loops over `Type.Fields()`, and
      the second removes them, so the commit has none.

## Phase 4: Bug fixes found during research

- [x] **`WithOptions` (`options.go:114`).** Copy every serializable field:
      `Privileged`, `Cmd`, `Entrypoint`, `DisableAutoCleanup`, `UseLocalImagesFirst`,
      `Auth`, `HostMounts`, `ExtraHosts`, `Reuse`, `CustomImage`, `User`. Then flip the
      Phase 1 golden test, and add the missing fields to `swagger.yaml`.
  - Merge semantics (implementation default): the 11 new fields apply only when set,
    so an empty `Options` doesn't undo options applied earlier. Bools are OR-ed,
    `HostMounts` entries are added (like `WithHostMounts`), and non-empty
    `Cmd`/`Entrypoint`/`ExtraHosts` and non-empty strings replace. The five old fields
    keep their semantics (`Debug`/`ContainerName` still always overwrite), as pinned by
    `TestWithOptionsMerge`; the new semantics are in `TestWithOptionsMergeSetFields`.
  - The golden test now asserts every field is copied. Swagger documents `entrypoint`,
    `host_mounts`, `extraHosts`, `reuse`, `customImage` and `user`, and the options
    allowlist in `TestSwaggerOptionsJSONTags` is gone. Verified end to end: a gnomockd
    `/start/memcached` request with `privileged`, `user`, `extraHosts`, `host_mounts`
    and `disable_cleanup` produced a container with all five applied.
- [x] **`pullImage` (`docker.go:101`).** Decode the JSON message stream and surface
      `{"error": ...}` entries instead of discarding them. Uses
      `ImagePullResponse.Wait`, which also types the error by its code (cerrdefs).
      `pull_internal_test.go` runs `pullImage` against a fake daemon (`httptest`); four
      of its cases fail on the old code.
- [x] **Error matching.** Replace daemon error-string matching with `cerrdefs`
      (`IsConflict` and friends) where the API supports it. Keep the Phase 1 tests.
      Expected flips: the "Current behavior" cases in `TestIsDeletionAlreadyInProgessError`
      (`docker_internal_test.go`) that return false for a missing client prefix or an
      extra wrap.
  - Both cases are 409 conflicts (checked against the local daemon for the duplicate
    name). Duplicate name: `duplicateContainerID` requires `IsConflict`, the regexp
    still extracts the ID, and an unrecognized conflict message falls back to removing
    by container name (within the `WithContainerName` contract). Removal in progress:
    the API has no specific type, so it is `IsConflict` plus a `strings.Contains` on
    the message.
  - The Phase 1 cases are all kept. Their errors are now wrapped in a conflict type, as
    moby/client returns them, and the two predicted flips happened. New cases: the same
    message without the conflict type is rejected, and `TestDuplicateContainerID`.
  - Not changed: `preset/postgres` matches the `pq` "database already exists" message.
    It is a driver error, not a daemon error, so it is outside this item.
- [x] **MySQL driver logger race** (found by the Phase 1 registry test under `-race`;
      fix approved by the owner).
      `preset/mysql/preset.go:72-74` and `preset/mariadb/preset.go:70-72` each call
      `mysqldriver.SetLogger` inside their own `sync.Once`. Concurrent `Options()` calls
      on both presets (gnomockd serving `/start/mysql` and `/start/mariadb` at once)
      race on the driver's global logger. Use one shared once, then restore
      `t.Parallel()` in the `TestPresets_defaultImage` subtests (`cmd/server/presets_test.go`).
  - New package `internal/mysqllog` (`Discard()`), added to `task test:core:internal`.
    The restored parallel test caught the race on the old code in 2 of 5 runs.
- [x] **Phase gate (local, amd64):** `go build`, `go vet` (amd64 and arm64),
      golangci-lint v2.15.0 (0 issues), the root package with `-race`, all
      `test:core:*` suites, and the mysql/mariadb presets plus their gnomockd tests.

## Phase 5: Major driver upgrades (internal only, no public API change)

- [x] **Redis.** `go-redis/redis/v7` → `github.com/redis/go-redis/v9` (v9.23.0), in the
      preset and its tests. The gnomockd test moves from `go-redis/redis` v6 to v9.
      The healthcheck and `WithValues` init now pass gnomock's context. Value encoding
      is unchanged (bools as `1`/`0`, JSON numbers in plain decimal). Passed against
      5.0.10, 6.0.9 and 7.2.4 with goleak.
- [x] **Mongo.** `go.mongodb.org/mongo-driver` v1 → `go.mongodb.org/mongo-driver/v2`
      (v2.10.0). The `bsonrw` package is gone in v2, so rework the data-loading code.
      ~~v2 needs server 4.2 or newer~~ **Correction:** v2.10 needs server **4.4** or
      newer (`MinSupportedMongoDBVersion`). That is still fine under the new matrix.
  - Data loader: `bson.NewExtJSONValueReader` + `bson.NewDecoder` (no error return),
    `mongo.Connect` without a context. Two small fixes along the way: data files are
    now closed (they were left open), and inserts use the init context instead of
    `context.Background()`.
  - The `3.6.21` test case is dropped now, because v2 can't talk to it. Phase 6 still
    sets the final matrix. New fixture `testdata/db2/events.json` checks that extended
    JSON types (`$oid`, `$date` in both forms, `$numberLong`) load as BSON types.
- [x] **Cassandra.** `github.com/gocql/gocql` → `github.com/apache/cassandra-gocql-driver/v2`
      (v2.1.2). Import path change only. Passed against 3 and 4.0 with goleak.
- [x] **Elastic.** Remove the `go-elasticsearch/v7` dependency and the v7 tests
      (`preset_v7_test.go`). The README row still lists 7.17.21; Phase 7 updates it.
- [x] **Phase gate (local, amd64):** build, `go vet` (amd64 and arm64), golangci-lint
      (0 issues), `GOWORK=off go build`, and the redis, mongo and cassandra presets plus
      their gnomockd tests. Elastic only compiles locally (see the disk risk). The root
      module no longer requires or builds any of the old drivers; `go list -m all` in
      workspace mode still shows them, through `cmd/cleaner`'s published gnomock
      v0.32.0 (the existing `cmd/cleaner` risk).
- [x] **CI (run 42441352310, Phase 5 head):** every job is green, including elastic, k3s
      and arm64, except the known localstack `0.12.2` case (see Risks).

## Phase 6: Presets (images, matrices, defaults)

| Preset | Default: old → new | Test matrix (new) | Extra work |
|---|---|---|---|
| postgres | 16.2 → **18** | 15, 16, 17, 18 | PG 18 changed the image's data-dir layout. Check the init/queries file handling. |
| mysql | 8.0.22 → **9.7** (LTS) | 8.4, 9.7, 26.10 | Delete `preset_arm64.go`/`preset_amd64.go` and use `library/mysql` everywhere. Delete the arm64 skips of the 5.7.32 case in `preset_test.go` and of gnomockd `TestMySQL` (Phase 2 known gaps). `mysql_native_password` is gone in 9.x, so verify `caching_sha2_password` over plain TCP. |
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
| azurite | 3.34.0 (`DefaultVersion`) → **3.37.0** | 3.37.0 | Already pinned; bump only. Bump `azblob` v1.6.1 → v1.7.0 in the same step (Phase 3 hold). |
| splunk | latest → **10.4** | 10.0, 10.2, 10.4 | amd64 only, document it. Add the `SPLUNK_GENERAL_TERMS` acceptance env (believed required since 9.4/10). |

### Progress

- [x] **localstack** (`6240b32`). Default `4.14`; matrix 3.8, 4.14, 2026.10 (needs
      `LOCALSTACK_AUTH_TOKEN`, skipped otherwise). Newest tags on 2026-11-08: 4.14.0
      and 2026.10.6, unchanged. `WithAuthToken` / JSON `auth_token` (documented in
      swagger in this commit, because the swagger tag test requires it). The healthcheck
      always uses `/_localstack/health`; the 0.x/1.x path selection and the unused
      `web` port (8080) are removed. Passed locally: preset (2026.10 skipped) and gnomockd.
- [x] **mysql** (`7d90546`). Default `9.7`; matrix 8.4, 9.7, 26.10 (newest: 8.4.13,
      9.7.4, 26.10.0). Per-arch split and both arm64 skips removed; gnomockd testdata asks
      for 8.4. `caching_sha2_password` over plain TCP works. Passed locally: preset and
      gnomockd.

For every row:

- [ ] Update the `defaultVersion` constant and the doc comments.
- [ ] Update the `preset_test.go` matrix.
- [ ] Update the gnomockd testdata JSON. It is stale today: kafka 2.5.1, localstack 0.11,
      elastic 8.7, and several `latest` tags. Also fix `vault.json`: it nests `options`
      inside `preset`, so the decoder silently ignores it. Then remove its two allowlist
      entries in `internal/gnomockd/swagger_test.go`.
- [ ] Add a `withDefaults` test where one is missing (azurite, cassandra, redis, splunk).
- [ ] Run the preset and gnomockd tests locally.

## Phase 7: API docs and generated code

- [ ] **`swagger/swagger.yaml`.** Update the default versions, the LocalStack token
      option, and the Kafka ZooKeeper deprecation. (The new `Options` fields were
      documented in Phase 4.)
- [ ] **Swagger gaps found in Phase 1** (allowlisted in `internal/gnomockd/swagger_test.go`):
      cockroachdb `db`/`queries`/`queries_files`, kafka `topic_configs`, the untagged k3s
      fields `Port`/`UseDynamicPort`/`K3sServerFlags` (document them under their Go names;
      adding JSON tags would change the wire format), named-port `host_port`, and
      `container` in the start-failed/stop-failed responses. Document them and shrink
      the allowlist.
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
   fields that used to be silently ignored. Go callers that pass a populated `Options`
   to `WithOptions` get those fields applied too. Unset fields don't override options
   applied earlier.
7. **Old servers.** The Mongo driver needs server 4.4 or newer, and EOL server versions are
   no longer tested.
8. **Init and healthcheck honor the start timeout.** The SQL presets (postgres, mysql,
   mariadb, mssql, cockroachdb) now run their healthcheck and init queries with the
   context gnomock passes in. An init (e.g. large `WithQueriesFile` migrations) that
   runs past `WithTimeout` now fails with a context error instead of finishing late.
9. **gnomockd router edge cases** (`net/http` instead of `gorilla/mux`). Routes, status
   codes for valid requests, and response bodies are unchanged. A wrong method still
   gets 405, now with an `Allow: POST` header and a `Method Not Allowed` body. Unclean
   paths (`//stop`) still redirect to the clean path, now with 307 (keeps the POST)
   instead of 301. A percent-encoded `/` or `.` in the preset name (`/start/a%2Fb`,
   `/start/%2e%2e`) now reaches the handler and gets the JSON "preset not found" 404,
   where mux gave a plain-text 404 or a redirect. 405 responses now carry
   `Content-Type: text/plain`.
10. **LocalStack ports.** The container no longer exposes the `web` port (8080), which
    only LocalStack before 0.11.3 served. Images before 1.3.0 are not supported.
11. **Pull failures fail at pull time.** An unknown tag or a denied pull now fails with
    `can't pull image: ...` and the daemon's message, instead of a later
    `can't create container: ... No such image`.

## Risks and open verification items

- **Kafka 4.x.** Unclear whether port 2181 is actually served. Decided empirically in
  Phase 6.
- **Splunk.** The `SPLUNK_GENERAL_TERMS` requirement is unconfirmed. Decided empirically.
- **LocalStack 4.x.** Possible healthcheck/endpoint changes.
- **k3s.** Recent versions on this cgroup v1 container host may fail locally. If so, rely
  on CI (cgroup v2).
- **Disk.** Splunk, mssql, elastic and k3s images are large, so prune between runs.
- **Docker Hub rate limit (local only).** Anonymous pulls from this session get HTTP 429.
  The local `dockerd` uses `mirror.gcr.io` as a registry mirror (session config in
  `/etc/docker/daemon.json`, not in the repo). The mirror can also return 429 for less
  popular images, which makes tests that always pull (e.g. `TestWaitForContainerNetwork`)
  flaky locally. Re-run before concluding a failure is real.
- **elastic and k3s can't pass locally** (found in Phase 3). The session's disk
  allowance (~13 GB free) is far smaller than the 252 GB filesystem `df` reports, so
  both see the disk as ~95% full. Elasticsearch refuses to allocate shards
  (`unavailable_shards_exception`, same failure at the pre-Phase-3 baseline `da8713e`),
  and k3s taints its node `disk-pressure`, so the kubeconfig pod never schedules. CI is
  the authority for these two presets.
- **localstack CI is red until Phase 6.** `[preset] localstack` fails on the
  `TestPreset_s3/0.12.2` case only: `localstack/localstack:0.12.2` has been deleted from
  Docker Hub (pull returns "not found"). Every other job was green on the Phase 4 run
  (42440676788). Phase 6 replaces the localstack matrix and removes the case.
- **Kafka CI flake.** `[preset] kafka` `TestPreset_withDefaults` (existing test) once hung
  in the healthcheck wait until the 10-minute test timeout (run 42405470107). It passed on
  re-run and locally in 58s. Re-run once before treating a kafka timeout as real; if it
  recurs, investigate (Phase 6 changes the kafka default anyway).
- **cmd/cleaner.** It still `require`s the published gnomock v0.32.0 for anyone building
  outside the workspace. `go.work` fixes local and CI builds only. Side effects seen in
  Phase 2: `./...` from the root now includes `cmd/cleaner` (so `go vet` covers it;
  golangci-lint still lints only the root module), and the cleaner's stale indirect
  requirements raise some unbuilt entries of the root module graph (e.g. grpc 1.67 →
  1.72). The packages actually built for the root module are identical with and without
  the workspace (`go list -deps -test`), and the server image is built with `GOWORK=off`.

## Out of scope

- InfluxDB 3 support.
- Publishing images to a registry.
- Renaming the module path.
- Moving from `lib/pq` to `pgx`.
- Upgrading from the Elastic v8 client to v9.
