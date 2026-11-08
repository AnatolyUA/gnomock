package localstack

import (
	"strings"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/stretchr/testify/require"
)

// TestOptions_authToken checks which LOCALSTACK_AUTH_TOKEN the container gets:
// the explicit token wins over the host's environment variable, and no
// variable is passed when neither is set.
func TestOptions_authToken(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		hostEnv string
		want    []string
	}{
		{name: "none", want: nil},
		{name: "explicit", token: "explicit", want: []string{"LOCALSTACK_AUTH_TOKEN=explicit"}},
		{name: "host env", hostEnv: "from-env", want: []string{"LOCALSTACK_AUTH_TOKEN=from-env"}},
		{
			name:    "explicit wins",
			token:   "explicit",
			hostEnv: "from-env",
			want:    []string{"LOCALSTACK_AUTH_TOKEN=explicit"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(authTokenEnv, tt.hostEnv)

			p := &P{Services: []Service{S3}}
			WithAuthToken(tt.token)(p)

			o := &gnomock.Options{}
			for _, opt := range p.Options() {
				opt(o)
			}

			var got []string

			for _, env := range o.Env {
				if strings.HasPrefix(env, authTokenEnv+"=") {
					got = append(got, env)
				}
			}

			require.Equal(t, tt.want, got)
			require.Equal(t, tt.token, p.AuthToken, "the host token must not be stored in the preset")
		})
	}
}
