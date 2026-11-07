package localstack_test

import (
	"encoding/json"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/localstack"
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
		defaultImage = "docker.io/localstack/localstack:0.14.0"
		customImage  = "docker.io/localstack/localstack:3.8"
	)

	tests := []struct {
		name string
		// preset builds the preset under test.
		preset func(t *testing.T) gnomock.Preset
		want   string
	}{
		{
			name:   "constructor default",
			preset: func(*testing.T) gnomock.Preset { return localstack.Preset() },
			want:   defaultImage,
		},
		{
			name: "WithVersion",
			preset: func(*testing.T) gnomock.Preset {
				return localstack.Preset(localstack.WithVersion("3.8"))
			},
			want: customImage,
		},
		{
			name:   "json empty object",
			preset: decodeLocalstackPreset(`{}`),
			want:   defaultImage,
		},
		{
			name:   "json version",
			preset: decodeLocalstackPreset(`{"version":"3.8"}`),
			want:   customImage,
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

// decodeLocalstackPreset returns a builder that decodes the given JSON into a
// zero-value preset, like gnomockd does with presets from the registry.
func decodeLocalstackPreset(body string) func(t *testing.T) gnomock.Preset {
	return func(t *testing.T) gnomock.Preset {
		t.Helper()

		p := &localstack.P{}
		require.NoError(t, json.Unmarshal([]byte(body), p))

		return p
	}
}
