package gnomock

import "strings"

const redacted = "[redacted]"

// secretEnvKeyParts are the parts of an environment variable name that mark
// its value as a secret, such as POSTGRES_PASSWORD, RABBITMQ_DEFAULT_PASS,
// LOCALSTACK_AUTH_TOKEN or AWS_SECRET_ACCESS_KEY. Matching is
// case-insensitive.
var secretEnvKeyParts = []string{
	"PASS", "TOKEN", "SECRET", "KEY", "AUTH", "CREDENTIAL", "ACCOUNTS",
}

// loggable returns a copy of o that is safe to write to the debug log:
// registry credentials and the values of secret-looking environment
// variables are replaced with a placeholder. The original Options, which go
// to the Docker API, are not modified.
func (o *Options) loggable() Options {
	c := *o

	if c.Auth != "" {
		c.Auth = redacted
	}

	if len(o.Env) > 0 {
		c.Env = make([]string, len(o.Env))
		for i, env := range o.Env {
			c.Env[i] = redactEnv(env)
		}
	}

	return c
}

// redactEnv replaces the value of a KEY=VALUE environment entry when its key
// looks like a secret.
func redactEnv(env string) string {
	key, _, ok := strings.Cut(env, "=")
	if !ok {
		return env
	}

	upper := strings.ToUpper(key)

	for _, part := range secretEnvKeyParts {
		if strings.Contains(upper, part) {
			return key + "=" + redacted
		}
	}

	return env
}
