// Package mysql provides a Gnomock Preset for MySQL database.
//
// This preset uses the official MySQL image (https://hub.docker.com/_/mysql)
// on every architecture. Tags older than about 8.0.29 have no arm64 image.
package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/mysqllog"
	"github.com/orlangure/gnomock/internal/registry"
)

const (
	defaultUser     = "gnomock"
	defaultPassword = "gnomick"
	defaultDatabase = "mydb"
	defaultPort     = 3306
	defaultVersion  = "9.7"
)

func init() {
	registry.Register("mysql", func() gnomock.Preset { return &P{} })
}

// Preset creates a new Gmomock MySQL preset. This preset includes a MySQL
// specific healthcheck function, default MySQL image and port, and allows to
// optionally set up initial state.
//
// When used without any configuration, it creates a superuser `gnomock` with
// password `gnomick`, and `mydb` database. Default MySQL version is 9.7 (LTS).
func Preset(opts ...Option) gnomock.Preset {
	p := &P{}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// P is a Gnomock Preset implementation of MySQL database.
type P struct {
	DB           string   `json:"db"`
	User         string   `json:"user"`
	Password     string   `json:"password"`
	Queries      []string `json:"queries"`
	QueriesFiles []string `json:"queries_files"`
	Version      string   `json:"version"`
}

// Image returns an image that should be pulled to create this container.
func (p *P) Image() string {
	return fmt.Sprintf("docker.io/library/mysql:%s", p.Version)
}

// Ports returns ports that should be used to access this container.
func (p *P) Ports() gnomock.NamedPorts {
	return gnomock.DefaultTCP(defaultPort)
}

// Options returns a list of options to configure this container.
func (p *P) Options() []gnomock.Option {
	mysqllog.Discard()

	p.setDefaults()

	opts := []gnomock.Option{
		gnomock.WithHealthCheck(p.healthcheck),
		gnomock.WithEnv("MYSQL_USER=" + p.User),
		gnomock.WithEnv("MYSQL_PASSWORD=" + p.Password),
		gnomock.WithEnv("MYSQL_DATABASE=" + p.DB),
		gnomock.WithEnv("MYSQL_RANDOM_ROOT_PASSWORD=yes"),
		gnomock.WithInit(p.initf()),
	}

	return opts
}

func (p *P) healthcheck(ctx context.Context, c *gnomock.Container) error {
	addr := c.Address(gnomock.DefaultPort)

	db, err := p.connect(ctx, addr)
	if err != nil {
		if db != nil {
			_ = db.Close()
		}

		return err
	}

	defer func() {
		_ = db.Close()
	}()

	var one int

	return db.QueryRowContext(ctx, `select 1`).Scan(&one)
}

func (p *P) initf() gnomock.InitFunc {
	return func(ctx context.Context, c *gnomock.Container) error {
		addr := c.Address(gnomock.DefaultPort)

		db, err := p.connect(ctx, addr)
		if err != nil {
			return err
		}

		defer func() { _ = db.Close() }()

		if len(p.QueriesFiles) > 0 {
			for _, f := range p.QueriesFiles {
				bs, err := os.ReadFile(f) // nolint:gosec
				if err != nil {
					return fmt.Errorf("can't read queries file '%s': %w", f, err)
				}

				p.Queries = append([]string{string(bs)}, p.Queries...)
			}
		}

		for _, q := range p.Queries {
			_, err = db.ExecContext(ctx, q)
			if err != nil {
				return err
			}
		}

		return nil
	}
}

func (p *P) connect(ctx context.Context, addr string) (*sql.DB, error) {
	connStr := fmt.Sprintf(
		"%s:%s@tcp(%s)/%s?multiStatements=true",
		p.User, p.Password, addr, p.DB,
	)

	db, err := sql.Open("mysql", connStr)
	if err != nil {
		return nil, err
	}

	return db, db.PingContext(ctx)
}

func (p *P) setDefaults() {
	if p.DB == "" {
		p.DB = defaultDatabase
	}

	if p.User == "" {
		p.User = defaultUser
	}

	if p.Password == "" {
		p.Password = defaultPassword
	}

	if p.Version == "" {
		p.Version = defaultVersion
	}
}
