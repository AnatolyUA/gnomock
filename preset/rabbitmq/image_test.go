package rabbitmq_test

import (
	"encoding/json"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/rabbitmq"
	"github.com/stretchr/testify/require"
)

// TestPreset_Image pins the image reference produced by every way of
// configuring the preset: the public constructor, WithVersion, and the JSON
// path used by gnomockd (a zero-value P decoded from the request body). The
// expected strings are hardcoded on purpose, so that changing a default image
// (PLAN.md Phase 6) has to update this test deliberately.
func TestPreset_Image(t *testing.T) {
	t.Parallel()

	const (
		defaultImage = "docker.io/library/rabbitmq:3.8.9"
		customImage  = "docker.io/library/rabbitmq:3.8.9-alpine"
	)

	tests := []struct {
		name string
		// preset builds the preset under test.
		preset func(t *testing.T) gnomock.Preset
		want   string
	}{
		{
			name:   "constructor default",
			preset: func(*testing.T) gnomock.Preset { return rabbitmq.Preset() },
			want:   defaultImage,
		},
		{
			name: "WithVersion",
			preset: func(*testing.T) gnomock.Preset {
				return rabbitmq.Preset(rabbitmq.WithVersion("3.8.9-alpine"))
			},
			want: customImage,
		},
		{
			name:   "json empty object",
			preset: decodeRabbitmqPreset(`{}`),
			want:   defaultImage,
		},
		{
			name:   "json version",
			preset: decodeRabbitmqPreset(`{"version":"3.8.9-alpine"}`),
			want:   customImage,
		},
		{
			// Management variants are selected by tag only.
			name: "WithVersion management",
			preset: func(*testing.T) gnomock.Preset {
				return rabbitmq.Preset(rabbitmq.WithVersion("3.8.9-management-alpine"))
			},
			want: "docker.io/library/rabbitmq:3.8.9-management-alpine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.preset(t)

			// Options() applies the defaults; gnomock.Start calls it before
			// Image().
			_ = p.Options()

			require.Equal(t, tt.want, p.Image())
		})
	}
}

// decodeRabbitmqPreset returns a builder that decodes the given JSON into a
// zero-value preset, like gnomockd does with presets from the registry.
func decodeRabbitmqPreset(body string) func(t *testing.T) gnomock.Preset {
	return func(t *testing.T) gnomock.Preset {
		t.Helper()

		p := &rabbitmq.P{}
		require.NoError(t, json.Unmarshal([]byte(body), p))

		return p
	}
}

// TestPreset_ImageManagementPorts pins that the management port is exposed
// only when the image tag selects a management variant.
func TestPreset_ImageManagementPorts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version        string
		wantManagement bool
	}{
		{version: "", wantManagement: false},
		{version: "3.8.9-alpine", wantManagement: false},
		{version: "3.8.9-management", wantManagement: true},
		{version: "3.8.9-management-alpine", wantManagement: true},
	}

	for _, tt := range tests {
		t.Run("version="+tt.version, func(t *testing.T) {
			t.Parallel()

			var opts []rabbitmq.Option
			if tt.version != "" {
				opts = append(opts, rabbitmq.WithVersion(tt.version))
			}

			p := rabbitmq.Preset(opts...)
			_ = p.Options()

			ports := p.Ports()
			_, ok := ports[rabbitmq.ManagementPort]
			require.Equal(t, tt.wantManagement, ok)
			require.Contains(t, ports, gnomock.DefaultPort)
		})
	}
}
