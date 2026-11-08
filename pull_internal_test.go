package gnomock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// fakePullDaemon serves the two endpoints pullImage uses: version negotiation
// and image create. The image create response is the given status and body.
func fakePullDaemon(t *testing.T, status int, body string) *docker {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/_ping") {
			w.Header().Set("Api-Version", "1.47")
			w.WriteHeader(http.StatusOK)

			return
		}

		if !strings.HasSuffix(r.URL.Path, "/images/create") {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	cli, err := client.New(client.WithHost("tcp://"+strings.TrimPrefix(srv.URL, "http://")), client.WithAPIVersion("1.47"))
	require.NoError(t, err)

	t.Cleanup(func() { _ = cli.Close() })

	return &docker{client: cli, log: zap.NewNop().Sugar()}
}

func TestPullImage(t *testing.T) {
	t.Parallel()

	const image = "docker.io/library/alpine:3"

	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
		errIs   func(error) bool
	}{
		{
			name:   "success",
			status: http.StatusOK,
			body: `{"status":"Pulling from library/alpine","id":"3"}` + "\n" +
				`{"status":"Download complete","id":"abc"}` + "\n" +
				`{"status":"Status: Downloaded newer image for alpine:3"}` + "\n",
		},
		{
			name:   "empty stream",
			status: http.StatusOK,
		},
		{
			// Before the fix, this pull "succeeded" and the failure surfaced
			// later as a confusing "No such image" from container create.
			name:   "error in stream",
			status: http.StatusOK,
			body: `{"status":"Pulling from library/alpine","id":"3"}` + "\n" +
				`{"errorDetail":{"message":"manifest for alpine:3 not found: manifest unknown"},` +
				`"error":"manifest for alpine:3 not found: manifest unknown"}` + "\n",
			wantErr: "manifest for alpine:3 not found: manifest unknown",
		},
		{
			name:   "error in stream with code",
			status: http.StatusOK,
			body: `{"errorDetail":{"code":401,"message":"unauthorized: authentication required"},` +
				`"error":"unauthorized: authentication required"}` + "\n",
			wantErr: "unauthorized: authentication required",
			errIs:   cerrdefs.IsUnauthorized,
		},
		{
			name:   "error after progress",
			status: http.StatusOK,
			body: `{"status":"Downloading","id":"abc","progressDetail":{"current":1,"total":2}}` + "\n" +
				`{"errorDetail":{"message":"unexpected EOF"},"error":"unexpected EOF"}` + "\n",
			wantErr: "unexpected EOF",
		},
		{
			name:    "malformed stream",
			status:  http.StatusOK,
			body:    `{"status":"Pulling`,
			wantErr: "can't read server output",
		},
		{
			name:    "http error",
			status:  http.StatusNotFound,
			body:    `{"message":"pull access denied for foo, repository does not exist"}`,
			wantErr: "pull access denied for foo",
			errIs:   cerrdefs.IsNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := fakePullDaemon(t, tt.status, tt.body)

			err := d.pullImage(context.Background(), image, &Options{})
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, tt.wantErr)

			if tt.errIs != nil {
				require.True(t, tt.errIs(err), err)
			}
		})
	}
}
