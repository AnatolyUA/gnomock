package main

import (
	"encoding/json"
	"runtime"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/registry"
	"github.com/orlangure/gnomock/preset/azurite"
	"github.com/orlangure/gnomock/preset/cassandra"
	"github.com/orlangure/gnomock/preset/cockroachdb"
	"github.com/orlangure/gnomock/preset/elastic"
	"github.com/orlangure/gnomock/preset/influxdb"
	"github.com/orlangure/gnomock/preset/k3s"
	"github.com/orlangure/gnomock/preset/kafka"
	"github.com/orlangure/gnomock/preset/localstack"
	"github.com/orlangure/gnomock/preset/mariadb"
	"github.com/orlangure/gnomock/preset/memcached"
	"github.com/orlangure/gnomock/preset/mongo"
	"github.com/orlangure/gnomock/preset/mssql"
	"github.com/orlangure/gnomock/preset/mysql"
	"github.com/orlangure/gnomock/preset/postgres"
	"github.com/orlangure/gnomock/preset/rabbitmq"
	"github.com/orlangure/gnomock/preset/redis"
	"github.com/orlangure/gnomock/preset/splunk"
	"github.com/orlangure/gnomock/preset/vault"
	"github.com/stretchr/testify/require"
)

// registeredPresets lists every preset gnomockd must serve, keyed by the name
// each preset passes to registry.Register. internal/registry has no listing
// function, so this explicit list is the source of truth for the count.
func registeredPresets() map[string]gnomock.Preset {
	return map[string]gnomock.Preset{
		"azurite":     &azurite.P{},
		"cassandra":   &cassandra.P{},
		"cockroachdb": &cockroachdb.P{},
		"elastic":     &elastic.P{},
		"influxdb":    &influxdb.P{},
		"kafka":       &kafka.P{},
		"kubernetes":  &k3s.P{},
		"localstack":  &localstack.P{},
		"mariadb":     &mariadb.P{},
		"memcached":   &memcached.P{},
		"mongo":       &mongo.P{},
		"mssql":       &mssql.P{},
		"mysql":       &mysql.P{},
		"postgres":    &postgres.P{},
		"rabbitmq":    &rabbitmq.P{},
		"redis":       &redis.P{},
		"splunk":      &splunk.P{},
		"vault":       &vault.P{},
	}
}

func TestPresets_registered(t *testing.T) {
	t.Parallel()

	presets := registeredPresets()
	require.Len(t, presets, 18)

	for name, want := range presets {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := registry.Find(name)
			require.NotNil(t, p)
			require.IsType(t, want, p)

			// gnomockd decodes every request into the preset it finds, so
			// each lookup must return a new instance.
			require.NotSame(t, p, registry.Find(name))
		})
	}
}

func TestPresets_unknown(t *testing.T) {
	t.Parallel()

	require.Nil(t, registry.Find("unknown"))
	require.Nil(t, registry.Find(""))
	require.Nil(t, registry.Find("k3s"), "k3s is registered as kubernetes")
}

// TestPresets_defaultImage pins the image gnomockd starts for a request with
// an empty preset object: registry.Find, JSON decoding of "{}" into the
// preset, then Options() before Image(), as gnomock.Start does. The expected
// strings are hardcoded on purpose, so that changing a default image (PLAN.md
// Phase 6) has to update this test deliberately.
func TestPresets_defaultImage(t *testing.T) {
	t.Parallel()

	mysqlImage := "docker.io/library/mysql:8.0.22"
	if runtime.GOARCH == "arm64" {
		// PLAN.md Phase 6 removes the per-arch split deliberately; update
		// this test then.
		mysqlImage = "docker.io/mysql/mysql-server:8.0.22"
	}

	tests := map[string]string{
		"azurite":     "mcr.microsoft.com/azure-storage/azurite:3.34.0",
		"cassandra":   "docker.io/library/cassandra:3",
		"cockroachdb": "docker.io/cockroachdb/cockroach:v25.1.6",
		"elastic":     "docker.io/library/elasticsearch:8.13.0",
		"influxdb":    "docker.io/influxdb:2.7.6-alpine",
		"kafka":       "docker.io/lensesio/fast-data-dev:3.6.1-L0",
		"kubernetes":  "docker.io/rancher/k3s:v1.26.3-k3s1",
		"localstack":  "docker.io/localstack/localstack:0.14.0",
		"mariadb":     "docker.io/library/mariadb:10.5.8",
		"memcached":   "docker.io/library/memcached:1.6.9",
		"mongo":       "docker.io/library/mongo:5.0",
		"mssql":       "mcr.microsoft.com/mssql/server:2019-latest",
		"mysql":       mysqlImage,
		"postgres":    "docker.io/library/postgres:16.2",
		"rabbitmq":    "docker.io/library/rabbitmq:3.8.9",
		"redis":       "docker.io/library/redis:6.0.9",
		"splunk":      "docker.io/splunk/splunk:latest",
		"vault":       "hashicorp/vault:1.13.0",
	}

	require.Len(t, tests, len(registeredPresets()))

	// The subtests run sequentially on purpose. Current behavior: the mysql
	// and mariadb presets each guard mysqldriver.SetLogger with their own
	// sync.Once, so calling Options() on both concurrently is a data race on
	// the driver's global logger (seen with -race). The race is pre-existing
	// in non-test code and out of scope for these safety-net tests.
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			p := registry.Find(name)
			require.NotNil(t, p)
			require.NoError(t, json.Unmarshal([]byte(`{}`), p))

			_ = p.Options()

			require.Equal(t, want, p.Image())
		})
	}
}
