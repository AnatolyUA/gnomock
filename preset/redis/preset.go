// Package redis includes Redis implementation of Gnomock Preset interface.
// This Preset can be passed to gnomock.Start() function to create a configured
// Redis container to use in tests.
package redis

import (
	"context"
	"fmt"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/registry"
	redisclient "github.com/redis/go-redis/v9"
)

const defaultVersion = "8.10"

func init() {
	registry.Register("redis", func() gnomock.Preset { return &P{} })
}

// Preset creates a new Gmomock Redis preset. This preset includes a Redis
// specific healthcheck function, default Redis image and port, and allows to
// optionally set up initial state. Default Redis version is 8.10.
func Preset(opts ...Option) gnomock.Preset {
	p := &P{}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// P is a Gnomock Preset implementation for Redis storage.
type P struct {
	Values  map[string]any `json:"values"`
	Version string         `json:"version"`
}

// Image returns an image that should be pulled to create this container.
func (p *P) Image() string {
	return fmt.Sprintf("docker.io/library/redis:%s", p.Version)
}

// Ports returns ports that should be used to access this container.
func (p *P) Ports() gnomock.NamedPorts {
	return gnomock.DefaultTCP(6379)
}

// Options returns a list of options to configure this container.
func (p *P) Options() []gnomock.Option {
	p.setDefaults()

	opts := []gnomock.Option{
		gnomock.WithHealthCheck(healthcheck),
	}

	if p.Values != nil {
		opts = append(opts, gnomock.WithInit(p.initf))
	}

	return opts
}

func (p *P) setDefaults() {
	if p.Version == "" {
		p.Version = defaultVersion
	}
}

func (p *P) initf(ctx context.Context, c *gnomock.Container) error {
	addr := c.Address(gnomock.DefaultPort)
	client := newClient(ctx, addr)

	defer func() { _ = client.Close() }()

	for k, v := range p.Values {
		err := client.Set(ctx, k, v, 0).Err()
		if err != nil {
			return fmt.Errorf("can't set '%s'='%v': %w", k, v, err)
		}
	}

	return nil
}

func healthcheck(ctx context.Context, c *gnomock.Container) error {
	addr := c.Address(gnomock.DefaultPort)
	client := newClient(ctx, addr)

	defer func() { _ = client.Close() }()

	_, err := client.Ping(ctx).Result()

	return err
}

// newClient returns a client whose commands honor the deadline and
// cancellation of ctx. Without ContextTimeoutEnabled, go-redis uses
// context.Background() for socket reads and writes, so a command could
// outlive gnomock's start timeout. That option only turns a deadline into
// socket deadlines, so the client is also closed when ctx is canceled, which
// unblocks a command waiting for a reply. Closing it again later is harmless.
func newClient(ctx context.Context, addr string) *redisclient.Client {
	client := redisclient.NewClient(&redisclient.Options{
		Addr:                  addr,
		ContextTimeoutEnabled: true,
	})

	context.AfterFunc(ctx, func() { _ = client.Close() })

	return client
}
