package k3s_test

import (
	"encoding/json"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/k3s"
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
		defaultImage = "docker.io/rancher/k3s:v1.37.2-k3s1"
		customImage  = "docker.io/rancher/k3s:v1.30.2-k3s1"
	)

	tests := []struct {
		name string
		// preset builds the preset under test.
		preset func(t *testing.T) gnomock.Preset
		want   string
	}{
		{
			name:   "constructor default",
			preset: func(*testing.T) gnomock.Preset { return k3s.Preset() },
			want:   defaultImage,
		},
		{
			name: "WithVersion",
			preset: func(*testing.T) gnomock.Preset {
				return k3s.Preset(k3s.WithVersion("v1.30.2-k3s1"))
			},
			want: customImage,
		},
		{
			name:   "json empty object",
			preset: decodeK3sPreset(`{}`),
			want:   defaultImage,
		},
		{
			name:   "json version",
			preset: decodeK3sPreset(`{"version":"v1.30.2-k3s1"}`),
			want:   customImage,
		},
		{
			// WithVersion appends "-k3s1" to versions without a k3s suffix.
			name: "WithVersion without k3s suffix",
			preset: func(*testing.T) gnomock.Preset {
				return k3s.Preset(k3s.WithVersion("v1.30.2"))
			},
			want: customImage,
		},
		{
			name: "WithVersion latest",
			preset: func(*testing.T) gnomock.Preset {
				return k3s.Preset(k3s.WithVersion("latest"))
			},
			want: "docker.io/rancher/k3s:latest",
		},
		{
			// Current behavior: the "-k3s1" suffix is added only by
			// WithVersion, so a version sent over JSON is used as is.
			name:   "json version without k3s suffix",
			preset: decodeK3sPreset(`{"version":"v1.30.2"}`),
			want:   "docker.io/rancher/k3s:v1.30.2",
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

// decodeK3sPreset returns a builder that decodes the given JSON into a
// zero-value preset, like gnomockd does with presets from the registry.
func decodeK3sPreset(body string) func(t *testing.T) gnomock.Preset {
	return func(t *testing.T) gnomock.Preset {
		t.Helper()

		p := &k3s.P{}
		require.NoError(t, json.Unmarshal([]byte(body), p))

		return p
	}
}
