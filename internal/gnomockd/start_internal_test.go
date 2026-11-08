package gnomockd

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/registry"
	"github.com/orlangure/gnomock/preset/postgres"
	"github.com/stretchr/testify/require"
)

const goldenStartRequest = "testdata/golden/start_request_all_options.json"

// withOptionsFields lists every exported gnomock.Options field. A new
// exported field fails the tests below until it is added here, to
// goldenOptions and to the golden start request, so that WithOptions can't
// silently drop it.
var withOptionsFields = []string{
	"Timeout",
	"Env",
	"Debug",
	"Privileged",
	"ContainerName",
	"Cmd",
	"Entrypoint",
	"HostMounts",
	"DisableAutoCleanup",
	"UseLocalImagesFirst",
	"CustomNamedPorts",
	"Auth",
	"ExtraHosts",
	"Reuse",
	"CustomImage",
	"User",
}

// goldenOptions is what the golden start request decodes into.
func goldenOptions() gnomock.Options {
	return gnomock.Options{
		Timeout:             60 * time.Second,
		Env:                 []string{"FOO=foo", "BAR=bar"},
		Debug:               true,
		Privileged:          true,
		ContainerName:       "gnomock-golden",
		Cmd:                 []string{"postgres", "-c", "fsync=off"},
		Entrypoint:          []string{"docker-entrypoint.sh"},
		HostMounts:          map[string]string{"/tmp/gnomock-src": "/data"},
		DisableAutoCleanup:  true,
		UseLocalImagesFirst: true,
		CustomNamedPorts: gnomock.NamedPorts{
			"default": {Protocol: "tcp", Port: 5432, HostPort: 15432},
		},
		Auth:        "eyJ1c2VybmFtZSI6ImZvbyIsInBhc3N3b3JkIjoiYmFyIn0K",
		ExtraHosts:  []string{"db.local:127.0.0.1"},
		Reuse:       true,
		CustomImage: "registry.example.com/library/postgres:16.2",
		User:        "1000:1000",
	}
}

// decodeStartRequest decodes body exactly like startHandler does.
func decodeStartRequest(t *testing.T, presetName string, body []byte) (*startRequest, error) {
	t.Helper()

	p := registry.Find(presetName)
	require.NotNil(t, p, presetName)

	sr := &startRequest{Preset: p}
	decoder := json.NewDecoder(bytes.NewReader(body))

	err := decoder.Decode(&sr)

	return sr, err
}

func exportedOptionsFields() []reflect.StructField {
	typ := reflect.TypeFor[gnomock.Options]()
	fields := make([]reflect.StructField, 0, typ.NumField())

	for f := range typ.Fields() {
		if f.IsExported() {
			fields = append(fields, f)
		}
	}

	return fields
}

func TestWithOptionsFieldListIsExhaustive(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(withOptionsFields))
	for _, f := range exportedOptionsFields() {
		names = append(names, f.Name)
	}

	require.ElementsMatch(t, withOptionsFields, names,
		"gnomock.Options exported fields changed: update withOptionsFields, goldenOptions and the golden start request")
}

func TestStartRequestGoldenOptions(t *testing.T) {
	t.Parallel()

	body, err := os.ReadFile(goldenStartRequest)
	require.NoError(t, err)

	t.Run("golden body has every options json key", func(t *testing.T) {
		t.Parallel()

		var raw struct {
			Options map[string]json.RawMessage `json:"options"`
		}

		require.NoError(t, json.Unmarshal(body, &raw))

		keys := make([]string, 0, len(raw.Options))
		for k := range raw.Options {
			keys = append(keys, k)
		}

		require.ElementsMatch(t, jsonFieldNames(t, reflect.TypeFor[gnomock.Options]()), keys)
	})

	t.Run("decoded like startHandler", func(t *testing.T) {
		t.Parallel()

		sr, err := decodeStartRequest(t, "postgres", body)
		require.NoError(t, err)

		// The preset is decoded into the registered concrete type.
		require.Equal(t, &postgres.P{
			Version:  "16.2",
			User:     "gnomock",
			Password: "gnomick",
			DB:       "golden",
		}, sr.Preset)

		// Timeout is encoded as an integer number of nanoseconds.
		require.Equal(t, 60*time.Second, sr.Options.Timeout)

		require.Equal(t, goldenOptions(), sr.Options)

		// Every exported field must be set by the golden body, so that the
		// WithOptions assertions below are meaningful.
		v := reflect.ValueOf(sr.Options)
		for _, f := range exportedOptionsFields() {
			require.False(t, v.FieldByIndex(f.Index).IsZero(), "golden body leaves %s unset", f.Name)
		}
	})

	t.Run("applied with WithOptions", func(t *testing.T) {
		t.Parallel()

		sr, err := decodeStartRequest(t, "postgres", body)
		require.NoError(t, err)

		got := &gnomock.Options{}
		gnomock.WithOptions(&sr.Options)(got)

		// Every field reaches the options gnomock.Start uses.
		src, dst := reflect.ValueOf(sr.Options), reflect.ValueOf(*got)

		for _, f := range exportedOptionsFields() {
			require.Equal(t, src.FieldByIndex(f.Index).Interface(), dst.FieldByIndex(f.Index).Interface(), f.Name)
		}

		require.Equal(t, goldenOptions(), *got)
	})
}

func TestStartRequestTimeoutEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		want    time.Duration
		wantErr bool
	}{
		{name: "nanoseconds", body: `{"options":{"timeout":30000000000}}`, want: 30 * time.Second},
		{name: "zero", body: `{"options":{"timeout":0}}`, want: 0},
		{name: "missing", body: `{"options":{}}`, want: 0},
		{
			// Current behavior: duration strings are rejected.
			name:    "duration string",
			body:    `{"options":{"timeout":"30s"}}`,
			wantErr: true,
		},
		{
			// Current behavior: fractional values are rejected.
			name:    "fractional",
			body:    `{"options":{"timeout":1.5}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sr, err := decodeStartRequest(t, "postgres", []byte(tt.body))

			if tt.wantErr {
				// Assert the error type rather than encoding/json's message,
				// which differs between Go versions (json/v2).
				var typeErr *json.UnmarshalTypeError

				require.ErrorAs(t, err, &typeErr)

				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, sr.Options.Timeout)
		})
	}

	bs, err := json.Marshal(gnomock.Options{Timeout: 30 * time.Second})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(string(bs), `{"timeout":30000000000,`), string(bs))
}

func TestWithOptionsMerge(t *testing.T) {
	t.Parallel()

	existing := func() *gnomock.Options {
		return &gnomock.Options{
			Timeout:          time.Minute,
			Env:              []string{"A=1"},
			Debug:            true,
			ContainerName:    "existing",
			CustomNamedPorts: gnomock.DefaultTCP(80),
		}
	}

	tests := []struct {
		name string
		in   gnomock.Options
		want gnomock.Options
	}{
		{
			name: "env is appended, not replaced",
			in:   gnomock.Options{Env: []string{"B=2", "C=3"}, Debug: true, ContainerName: "existing"},
			want: gnomock.Options{
				Timeout:          time.Minute,
				Env:              []string{"A=1", "B=2", "C=3"},
				Debug:            true,
				ContainerName:    "existing",
				CustomNamedPorts: gnomock.DefaultTCP(80),
			},
		},
		{
			name: "zero timeout and nil ports don't override",
			in:   gnomock.Options{Debug: true, ContainerName: "existing"},
			want: gnomock.Options{
				Timeout:          time.Minute,
				Env:              []string{"A=1"},
				Debug:            true,
				ContainerName:    "existing",
				CustomNamedPorts: gnomock.DefaultTCP(80),
			},
		},
		{
			name: "non-zero timeout and ports override",
			in: gnomock.Options{
				Timeout:          time.Second,
				CustomNamedPorts: gnomock.DefaultTCP(8080),
				Debug:            true,
				ContainerName:    "existing",
			},
			want: gnomock.Options{
				Timeout:          time.Second,
				Env:              []string{"A=1"},
				Debug:            true,
				ContainerName:    "existing",
				CustomNamedPorts: gnomock.DefaultTCP(8080),
			},
		},
		{
			// Current behavior: an empty but non-nil map replaces the
			// existing ports.
			name: "empty non-nil ports override",
			in:   gnomock.Options{CustomNamedPorts: gnomock.NamedPorts{}, Debug: true, ContainerName: "existing"},
			want: gnomock.Options{
				Timeout:          time.Minute,
				Env:              []string{"A=1"},
				Debug:            true,
				ContainerName:    "existing",
				CustomNamedPorts: gnomock.NamedPorts{},
			},
		},
		{
			// Current behavior: Debug and ContainerName are always
			// overwritten, even with zero values.
			name: "zero debug and container name override",
			in:   gnomock.Options{},
			want: gnomock.Options{
				Timeout:          time.Minute,
				Env:              []string{"A=1"},
				CustomNamedPorts: gnomock.DefaultTCP(80),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := existing()
			in := tt.in
			gnomock.WithOptions(&in)(got)

			require.Equal(t, tt.want, *got)
		})
	}
}

// TestWithOptionsMergeSetFields covers the fields that WithOptions applies
// only when they are set, on top of options applied earlier.
func TestWithOptionsMergeSetFields(t *testing.T) {
	t.Parallel()

	existing := func() *gnomock.Options {
		return &gnomock.Options{
			Privileged:          true,
			Cmd:                 []string{"old-cmd"},
			Entrypoint:          []string{"old-entrypoint"},
			HostMounts:          map[string]string{"/old": "/old", "/shared": "/old-shared"},
			DisableAutoCleanup:  true,
			UseLocalImagesFirst: true,
			Auth:                "old-auth",
			ExtraHosts:          []string{"old.local:127.0.0.1"},
			Reuse:               true,
			CustomImage:         "old/image:1",
			User:                "1:1",
		}
	}

	tests := []struct {
		name     string
		existing *gnomock.Options
		in       gnomock.Options
		want     gnomock.Options
	}{
		{
			name:     "zero values don't override",
			existing: existing(),
			in:       gnomock.Options{},
			want:     *existing(),
		},
		{
			name:     "set values override, host mounts are merged",
			existing: existing(),
			in: gnomock.Options{
				Cmd:         []string{"new-cmd", "arg"},
				Entrypoint:  []string{"new-entrypoint"},
				HostMounts:  map[string]string{"/new": "/new", "/shared": "/new-shared"},
				Auth:        "new-auth",
				ExtraHosts:  []string{"new.local:127.0.0.1"},
				CustomImage: "new/image:2",
				User:        "2:2",
			},
			want: gnomock.Options{
				Privileged:          true,
				Cmd:                 []string{"new-cmd", "arg"},
				Entrypoint:          []string{"new-entrypoint"},
				HostMounts:          map[string]string{"/old": "/old", "/new": "/new", "/shared": "/new-shared"},
				DisableAutoCleanup:  true,
				UseLocalImagesFirst: true,
				Auth:                "new-auth",
				ExtraHosts:          []string{"new.local:127.0.0.1"},
				Reuse:               true,
				CustomImage:         "new/image:2",
				User:                "2:2",
			},
		},
		{
			name:     "set values apply to empty options",
			existing: &gnomock.Options{},
			in: gnomock.Options{
				Privileged:          true,
				HostMounts:          map[string]string{"/src": "/dst"},
				DisableAutoCleanup:  true,
				UseLocalImagesFirst: true,
				Reuse:               true,
			},
			want: gnomock.Options{
				Privileged:          true,
				HostMounts:          map[string]string{"/src": "/dst"},
				DisableAutoCleanup:  true,
				UseLocalImagesFirst: true,
				Reuse:               true,
			},
		},
		{
			name:     "empty slices and maps don't override",
			existing: existing(),
			in: gnomock.Options{
				Cmd:        []string{},
				Entrypoint: []string{},
				HostMounts: map[string]string{},
				ExtraHosts: []string{},
			},
			want: *existing(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := tt.in
			gnomock.WithOptions(&in)(tt.existing)

			require.Equal(t, tt.want, *tt.existing)
		})
	}

	t.Run("source host mounts are not aliased", func(t *testing.T) {
		t.Parallel()

		src := &gnomock.Options{HostMounts: map[string]string{"/src": "/dst"}}
		dst := &gnomock.Options{}
		gnomock.WithOptions(src)(dst)

		dst.HostMounts["/other"] = "/other"

		require.Equal(t, map[string]string{"/src": "/dst"}, src.HostMounts)
	})
}

func TestContainerResponseJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		container *gnomock.Container
		want      string
	}{
		{
			name: "full",
			container: &gnomock.Container{
				ID:   "f5d08dc84421",
				Host: "127.0.0.1",
				Ports: gnomock.NamedPorts{
					"default": gnomock.TCP(35973),
					"udp":     {Protocol: "udp", Port: 35974},
				},
			},
			want: `{"id":"f5d08dc84421","host":"127.0.0.1","ports":{` +
				`"default":{"protocol":"tcp","port":35973,"host_port":0},` +
				`"udp":{"protocol":"udp","port":35974,"host_port":0}}}`,
		},
		{
			name:      "empty",
			container: &gnomock.Container{},
			want:      `{}`,
		},
		{
			name:      "empty ports map is omitted",
			container: &gnomock.Container{ID: "abc", Ports: gnomock.NamedPorts{}},
			want:      `{"id":"abc"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// startHandler responds with json.NewEncoder(w).Encode(c).
			buf := &bytes.Buffer{}
			require.NoError(t, json.NewEncoder(buf).Encode(tt.container))
			require.Equal(t, tt.want+"\n", buf.String())

			// Clients decode the same shape back.
			var decoded gnomock.Container

			require.NoError(t, json.Unmarshal([]byte(tt.want), &decoded))
			require.Equal(t, tt.container.ID, decoded.ID)
			require.Equal(t, tt.container.Host, decoded.Host)

			if len(tt.container.Ports) > 0 {
				require.Equal(t, tt.container.Ports, decoded.Ports)
			} else {
				require.Empty(t, decoded.Ports)
			}
		})
	}
}
