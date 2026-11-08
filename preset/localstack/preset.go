// Package localstack provides a Gnomock Preset for localstack project
// (https://github.com/localstack/localstack). It allows to easily setup local
// AWS stack for testing
package localstack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/registry"
)

// APIPort should be used to configure AWS SDK endpoint.
const APIPort = "api"

// defaultVersion is the last LocalStack release that runs without an auth
// token. Newer (2026.x) images need one, see WithAuthToken.
const defaultVersion = "4.14"

// authTokenEnv is the environment variable LocalStack reads its auth token
// from. The preset also falls back to it on the host, like LocalStack's own
// tooling does.
const authTokenEnv = "LOCALSTACK_AUTH_TOKEN" //nolint:gosec // variable name, not a credential

func init() {
	registry.Register("localstack", func() gnomock.Preset { return &P{} })
}

// Preset creates a new localstack preset to use with gnomock.Start. See
// package docs for a list of exposed ports and services. It is legal to not
// provide any services using WithServices options, but in such case a new
// localstack container will be useless.
//
// This Preset cannot be used with localstack images prior to 1.3.0. It is
// tested with 3.8, 4.14 and 2026.x (with an auth token).
func Preset(opts ...Option) gnomock.Preset {
	p := &P{}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// P is a Gnomock Preset localstack implementation.
type P struct {
	Services  []Service `json:"services"`
	S3Path    string    `json:"s3_path"`
	Version   string    `json:"version"`
	AuthToken string    `json:"auth_token"`
}

// Image returns an image that should be pulled to create this container.
func (p *P) Image() string {
	return fmt.Sprintf("docker.io/localstack/localstack:%s", p.Version)
}

// Ports returns ports that should be used to access this container.
func (p *P) Ports() gnomock.NamedPorts {
	return gnomock.NamedPorts{
		APIPort: {Protocol: "tcp", Port: 4566},
	}
}

// Options returns a list of options to configure this container.
func (p *P) Options() []gnomock.Option {
	p.setDefaults()

	svcStrings := make([]string, len(p.Services))
	for i, svc := range p.Services {
		svcStrings[i] = string(svc)
	}

	svcEnv := strings.Join(svcStrings, ",")

	opts := []gnomock.Option{
		gnomock.WithHealthCheck(p.healthcheck(svcStrings)),
		gnomock.WithEnv("SERVICES=" + svcEnv),
		gnomock.WithInit(p.initf()),
	}

	if token := p.authToken(); token != "" {
		opts = append(opts, gnomock.WithEnv(authTokenEnv+"="+token))
	}

	return opts
}

// authToken returns the configured auth token, or the host's
// LOCALSTACK_AUTH_TOKEN when none is configured. The host value is not stored
// in the preset, so it never ends up in serialized presets.
func (p *P) authToken() string {
	if p.AuthToken != "" {
		return p.AuthToken
	}

	return os.Getenv(authTokenEnv)
}

func (p *P) setDefaults() {
	if p.Version == "" {
		p.Version = defaultVersion
	}
}

func (p *P) healthcheck(services []string) gnomock.HealthcheckFunc {
	return func(ctx context.Context, c *gnomock.Container) (err error) {
		addr := fmt.Sprintf("http://%s/_localstack/health", c.Address(APIPort))

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
		if err != nil {
			return err
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}

		defer func() {
			closeErr := res.Body.Close()
			if err == nil && closeErr != nil {
				err = closeErr
			}
		}()

		var hr healthResponse

		decoder := json.NewDecoder(res.Body)

		err = decoder.Decode(&hr)
		if err != nil {
			return err
		}

		if len(hr.Services) < len(services) {
			return fmt.Errorf(
				"not enough active services: want %d got %d [%s]",
				len(services), len(hr.Services), hr.Services,
			)
		}

		for _, service := range services {
			status := hr.Services[service]
			// available status means that the service is lazy loaded on the
			// first request
			if status != "running" && status != "available" {
				return fmt.Errorf("service '%s' is not running", service)
			}
		}

		return nil
	}
}

type healthResponse struct {
	Services map[string]string `json:"services"`
}

func (p *P) initf() gnomock.InitFunc {
	return func(_ context.Context, c *gnomock.Container) error {
		for _, s := range p.Services {
			if s == S3 {
				err := p.initS3(c)
				if err != nil {
					return fmt.Errorf("can't init s3 storage: %w", err)
				}
			}
		}

		return nil
	}
}
