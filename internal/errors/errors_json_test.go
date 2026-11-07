package errors_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/errors"
	"github.com/stretchr/testify/require"
)

func TestErrorsJSON(t *testing.T) {
	t.Parallel()

	rootErr := fmt.Errorf("boom")
	container := &gnomock.Container{
		ID:    "f5d08dc84421",
		Host:  "127.0.0.1",
		Ports: gnomock.DefaultTCP(35973),
	}
	containerJSON := `{"id":"f5d08dc84421","host":"127.0.0.1",` +
		`"ports":{"default":{"protocol":"tcp","port":35973,"host_port":0}}}`

	tests := []struct {
		name string
		err  error
		json string
		code int
	}{
		{
			name: "preset not found",
			err:  errors.NewPresetNotFoundError("foobar"),
			json: `{"error":"preset 'foobar' not found"}`,
			code: http.StatusNotFound,
		},
		{
			name: "invalid start request",
			err:  errors.NewInvalidStartRequestError(rootErr),
			json: `{"error":"invalid start request: boom"}`,
			code: http.StatusBadRequest,
		},
		{
			name: "start failed without container",
			err:  errors.NewStartFailedError(rootErr, nil),
			json: `{"error":"start failed: boom"}`,
			code: http.StatusInternalServerError,
		},
		{
			// A non-nil empty container is not omitted.
			name: "start failed with empty container",
			err:  errors.NewStartFailedError(rootErr, &gnomock.Container{}),
			json: `{"error":"start failed: boom","container":{}}`,
			code: http.StatusInternalServerError,
		},
		{
			name: "start failed with container",
			err:  errors.NewStartFailedError(rootErr, container),
			json: `{"error":"start failed: boom","container":` + containerJSON + `}`,
			code: http.StatusInternalServerError,
		},
		{
			name: "invalid stop request",
			err:  errors.InvalidStopRequestError(rootErr),
			json: `{"error":"invalid stop request: boom"}`,
			code: http.StatusBadRequest,
		},
		{
			name: "stop failed without container",
			err:  errors.StopFailedError(rootErr, nil),
			json: `{"error":"stop failed: boom"}`,
			code: http.StatusInternalServerError,
		},
		{
			name: "stop failed with container",
			err:  errors.StopFailedError(rootErr, container),
			json: `{"error":"stop failed: boom","container":` + containerJSON + `}`,
			code: http.StatusInternalServerError,
		},
		{
			// Current behavior: wrapping keeps the status code, but the
			// wrapper itself has no exported fields to encode.
			name: "wrapped invalid start request",
			err:  fmt.Errorf("wrapped: %w", errors.NewInvalidStartRequestError(rootErr)),
			json: `{}`,
			code: http.StatusBadRequest,
		},
		{
			name: "wrapped preset not found",
			err:  fmt.Errorf("wrapped: %w", errors.NewPresetNotFoundError("foobar")),
			json: `{}`,
			code: http.StatusNotFound,
		},
		{
			name: "unknown error",
			err:  rootErr,
			json: `{}`,
			code: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bs, err := json.Marshal(tt.err)
			require.NoError(t, err)
			require.Equal(t, tt.json, string(bs))
			require.Equal(t, tt.code, errors.ErrorCode(tt.err))

			// gnomockd responds with json.NewEncoder(w).Encode(err).
			buf := &bytes.Buffer{}
			require.NoError(t, json.NewEncoder(buf).Encode(tt.err))
			require.Equal(t, tt.json+"\n", buf.String())
		})
	}
}

func TestErrorsJSONEscaping(t *testing.T) {
	t.Parallel()

	// The error text is encoded with encoding/json defaults, including HTML
	// escaping.
	err := errors.NewStartFailedError(fmt.Errorf(`<logs> "quoted" & done`), nil)

	bs, jsonErr := json.Marshal(err)
	require.NoError(t, jsonErr)
	require.Equal(t, `{"error":"start failed: \u003clogs\u003e \"quoted\" \u0026 done"}`, string(bs))
}
