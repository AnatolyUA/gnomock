package gnomock

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestRedactEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		env  string
		want string
	}{
		{"LOCALSTACK_AUTH_TOKEN=ls-secret", "LOCALSTACK_AUTH_TOKEN=[redacted]"},
		{"POSTGRES_PASSWORD=pg-secret", "POSTGRES_PASSWORD=[redacted]"},
		{"RABBITMQ_DEFAULT_PASS=rmq-secret", "RABBITMQ_DEFAULT_PASS=[redacted]"},
		{"VAULT_DEV_ROOT_TOKEN_ID=root", "VAULT_DEV_ROOT_TOKEN_ID=[redacted]"},
		{"AWS_SECRET_ACCESS_KEY=aws-secret", "AWS_SECRET_ACCESS_KEY=[redacted]"},
		{"AZURITE_ACCOUNTS=acc:key", "AZURITE_ACCOUNTS=[redacted]"},
		{"api_key=lower", "api_key=[redacted]"},
		{"TOKEN=", "TOKEN=[redacted]"},
		{"POSTGRES_USER=gnomock", "POSTGRES_USER=gnomock"},
		{"SERVICES=s3,sqs", "SERVICES=s3,sqs"},
		{"TZ=", "TZ="},
		{"NO_EQUALS_SIGN", "NO_EQUALS_SIGN"},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, redactEnv(tt.env))
		})
	}
}

func TestOptionsLoggable(t *testing.T) {
	t.Parallel()

	const (
		token    = "sentinel-localstack-token"
		password = "sentinel-db-password"
		auth     = "c2VudGluZWwtcmVnaXN0cnktYXV0aA==" // sentinel-registry-auth
	)

	o := &Options{
		Env: []string{
			"SERVICES=s3",
			"LOCALSTACK_AUTH_TOKEN=" + token,
			"POSTGRES_PASSWORD=" + password,
		},
		Auth:          auth,
		ContainerName: "visible-name",
		User:          "1000:1000",
	}

	before := append([]string(nil), o.Env...)

	l := o.loggable()

	// The real options, which go to the Docker API, keep every value.
	require.Equal(t, before, o.Env)
	require.Equal(t, auth, o.Auth)

	require.Equal(t, []string{
		"SERVICES=s3",
		"LOCALSTACK_AUTH_TOKEN=[redacted]",
		"POSTGRES_PASSWORD=[redacted]",
	}, l.Env)
	require.Equal(t, redacted, l.Auth)

	// Encode the copy the way the debug logger does, and check that no secret
	// reaches the output while the other fields still do.
	var buf bytes.Buffer

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig()),
		zapcore.AddSync(&buf),
		zapcore.DebugLevel,
	)
	zap.New(core).Sugar().Infow("using config", "config", l)

	out := buf.String()
	require.NotContains(t, out, token)
	require.NotContains(t, out, password)
	require.NotContains(t, out, auth)
	require.Contains(t, out, "visible-name")
	require.Contains(t, out, "SERVICES=s3")
	require.Contains(t, out, "LOCALSTACK_AUTH_TOKEN=[redacted]")
}

func TestOptionsLoggable_empty(t *testing.T) {
	t.Parallel()

	l := (&Options{}).loggable()
	require.Empty(t, l.Auth)
	require.Nil(t, l.Env)
}
