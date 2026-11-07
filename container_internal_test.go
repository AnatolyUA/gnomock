package gnomock

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		id      string
		sidecar string
		want    string
	}{
		{
			name:    "long ids are truncated to 10 characters each",
			id:      "0123456789abcdef0123456789abcdef",
			sidecar: "fedcba9876543210fedcba9876543210",
			want:    "0123456789-fedcba9876",
		},
		{
			name:    "ids of exactly 10 characters are kept",
			id:      "0123456789",
			sidecar: "abcdefghij",
			want:    "0123456789-abcdefghij",
		},
		{
			name:    "short ids are kept",
			id:      "abc",
			sidecar: "def",
			want:    "abc-def",
		},
		{
			// Current behavior: an empty sidecar still produces a trailing
			// separator.
			name:    "empty sidecar",
			id:      "abc",
			sidecar: "",
			want:    "abc-",
		},
		{
			name:    "empty id",
			id:      "",
			sidecar: "abc",
			want:    "-abc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, generateID(tt.id, tt.sidecar))
		})
	}
}

func TestParseID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantID      string
		wantSidecar string
	}{
		{
			name:        "id with sidecar",
			input:       "0123456789-fedcba9876",
			wantID:      "0123456789",
			wantSidecar: "fedcba9876",
		},
		{
			name:        "full docker id without separator",
			input:       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantID:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			wantSidecar: "",
		},
		{
			name:        "more than one separator returns the input as is",
			input:       "a-b-c",
			wantID:      "a-b-c",
			wantSidecar: "",
		},
		{
			// Current behavior: a trailing separator (generateID with an
			// empty sidecar) yields the id and an empty sidecar.
			name:        "trailing separator",
			input:       "abc-",
			wantID:      "abc",
			wantSidecar: "",
		},
		{
			name:        "leading separator",
			input:       "-abc",
			wantID:      "",
			wantSidecar: "abc",
		},
		{
			name:        "empty input",
			input:       "",
			wantID:      "",
			wantSidecar: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id, sidecar := parseID(tt.input)
			require.Equal(t, tt.wantID, id)
			require.Equal(t, tt.wantSidecar, sidecar)
		})
	}
}

func TestGenerateIDParseIDRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		id          string
		sidecar     string
		wantID      string
		wantSidecar string
	}{
		{
			name:        "long ids",
			id:          "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			sidecar:     "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
			wantID:      "0123456789",
			wantSidecar: "fedcba9876",
		},
		{
			name:        "short ids",
			id:          "abc",
			sidecar:     "def",
			wantID:      "abc",
			wantSidecar: "def",
		},
		{
			name:        "empty sidecar",
			id:          "abc",
			sidecar:     "",
			wantID:      "abc",
			wantSidecar: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			id, sidecar := parseID(generateID(tt.id, tt.sidecar))
			require.Equal(t, tt.wantID, id)
			require.Equal(t, tt.wantSidecar, sidecar)
		})
	}
}

func TestContainer_DockerID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
		want string
	}{
		{
			name: "id with sidecar",
			id:   "0123456789-fedcba9876",
			want: "0123456789",
		},
		{
			name: "id without sidecar",
			id:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			want: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		},
		{
			name: "id with empty sidecar",
			id:   "abc-",
			want: "abc",
		},
		{
			name: "id with more than one separator",
			id:   "a-b-c",
			want: "a-b-c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := &Container{ID: tt.id}
			require.Equal(t, tt.want, c.DockerID())
		})
	}
}
