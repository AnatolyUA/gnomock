package elastic_test

import (
	"encoding/json"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/elastic"
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
		defaultImage = "docker.io/library/elasticsearch:8.13.0"
		customImage  = "docker.io/library/elasticsearch:8.19.2"
	)

	tests := []struct {
		name string
		// preset builds the preset under test.
		preset func(t *testing.T) gnomock.Preset
		// callOptions mirrors gnomock.Start, which calls Options() before
		// Image().
		callOptions bool
		want        string
	}{
		{
			// Current behavior: defaults are applied lazily in Options(), so
			// Image() alone has an empty tag. gnomock.Start always calls
			// Options() first.
			name:        "constructor without Options",
			preset:      func(*testing.T) gnomock.Preset { return elastic.Preset() },
			callOptions: false,
			want:        "docker.io/library/elasticsearch:",
		},
		{
			name:        "constructor default",
			preset:      func(*testing.T) gnomock.Preset { return elastic.Preset() },
			callOptions: true,
			want:        defaultImage,
		},
		{
			name: "WithVersion",
			preset: func(*testing.T) gnomock.Preset {
				return elastic.Preset(elastic.WithVersion("8.19.2"))
			},
			callOptions: true,
			want:        customImage,
		},
		{
			name:        "json empty object",
			preset:      decodeElasticPreset(`{}`),
			callOptions: true,
			want:        defaultImage,
		},
		{
			name:        "json version",
			preset:      decodeElasticPreset(`{"version":"8.19.2"}`),
			callOptions: true,
			want:        customImage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := tt.preset(t)

			if tt.callOptions {
				_ = p.Options()
			}

			require.Equal(t, tt.want, p.Image())
		})
	}
}

// decodeElasticPreset returns a builder that decodes the given JSON into a
// zero-value preset, like gnomockd does with presets from the registry.
func decodeElasticPreset(body string) func(t *testing.T) gnomock.Preset {
	return func(t *testing.T) gnomock.Preset {
		t.Helper()

		p := &elastic.P{}
		require.NoError(t, json.Unmarshal([]byte(body), p))

		return p
	}
}
