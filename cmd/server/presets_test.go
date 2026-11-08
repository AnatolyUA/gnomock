package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/orlangure/gnomock/internal/registry"
	"github.com/stretchr/testify/require"
)

// registeredPresets lists every preset gnomockd must serve, keyed by the name
// each preset passes to registry.Register, with the package that implements
// it. internal/registry has no listing function, so this explicit list is the
// source of truth for the count.
//
// This file deliberately does not import any preset package: the registry
// must be populated only by the blank imports in presets.go, otherwise a
// preset missing from presets.go would go unnoticed.
func registeredPresets() map[string]string {
	const prefix = "github.com/orlangure/gnomock/preset/"

	return map[string]string{
		"azurite":     prefix + "azurite",
		"cassandra":   prefix + "cassandra",
		"cockroachdb": prefix + "cockroachdb",
		"elastic":     prefix + "elastic",
		"influxdb":    prefix + "influxdb",
		"kafka":       prefix + "kafka",
		"kubernetes":  prefix + "k3s",
		"localstack":  prefix + "localstack",
		"mariadb":     prefix + "mariadb",
		"memcached":   prefix + "memcached",
		"mongo":       prefix + "mongo",
		"mssql":       prefix + "mssql",
		"mysql":       prefix + "mysql",
		"postgres":    prefix + "postgres",
		"rabbitmq":    prefix + "rabbitmq",
		"redis":       prefix + "redis",
		"splunk":      prefix + "splunk",
		"vault":       prefix + "vault",
	}
}

func TestPresets_registered(t *testing.T) {
	t.Parallel()

	presets := registeredPresets()
	require.Len(t, presets, 18)

	for name, wantPkg := range presets {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := registry.Find(name)
			require.NotNil(t, p, "preset %q is not registered; is it imported in presets.go?", name)

			typ := reflect.TypeOf(p)
			require.Equal(t, reflect.Pointer, typ.Kind())
			require.Equal(t, wantPkg, typ.Elem().PkgPath())
			require.Equal(t, "P", typ.Elem().Name())

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

	tests := map[string]string{
		"azurite":     "mcr.microsoft.com/azure-storage/azurite:3.34.0",
		"cassandra":   "docker.io/library/cassandra:3",
		"cockroachdb": "docker.io/cockroachdb/cockroach:v25.1.6",
		"elastic":     "docker.io/library/elasticsearch:8.13.0",
		"influxdb":    "docker.io/influxdb:2.7.6-alpine",
		"kafka":       "docker.io/lensesio/fast-data-dev:3.6.1-L0",
		"kubernetes":  "docker.io/rancher/k3s:v1.26.3-k3s1",
		"localstack":  "docker.io/localstack/localstack:4.14",
		"mariadb":     "docker.io/library/mariadb:12.3",
		"memcached":   "docker.io/library/memcached:1.6.9",
		"mongo":       "docker.io/library/mongo:5.0",
		"mssql":       "mcr.microsoft.com/mssql/server:2019-latest",
		"mysql":       "docker.io/library/mysql:9.7",
		"postgres":    "docker.io/library/postgres:18",
		"rabbitmq":    "docker.io/library/rabbitmq:3.8.9",
		"redis":       "docker.io/library/redis:8.10",
		"splunk":      "docker.io/splunk/splunk:latest",
		"vault":       "hashicorp/vault:1.13.0",
	}

	require.Len(t, tests, len(registeredPresets()))

	// The subtests run in parallel on purpose: under -race they catch shared
	// global state in Options(), like the MySQL driver logger that the mysql
	// and mariadb presets used to set behind separate sync.Once guards.
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			p := registry.Find(name)
			require.NotNil(t, p)
			require.NoError(t, json.Unmarshal([]byte(`{}`), p))

			_ = p.Options()

			require.Equal(t, want, p.Image())
		})
	}
}
