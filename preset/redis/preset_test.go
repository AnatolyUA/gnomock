package redis_test

import (
	"context"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/redis"
	redisclient "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestPreset(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"5.0.10", "6.0.9", "7.2.4"} {
		t.Run(version, testPreset(version))
	}
}

func testPreset(version string) func(t *testing.T) {
	return func(t *testing.T) {
		vs := make(map[string]any)

		vs["a"] = "foo"
		vs["b"] = 42
		vs["c"] = true

		p := redis.Preset(
			redis.WithValues(vs),
			redis.WithVersion(version),
		)
		container, err := gnomock.Start(p)

		defer func() { require.NoError(t, gnomock.Stop(container)) }()

		require.NoError(t, err)

		ctx := context.Background()
		addr := container.DefaultAddress()
		client := redisclient.NewClient(&redisclient.Options{Addr: addr})

		var str string

		require.NoError(t, client.Get(ctx, "a").Scan(&str))
		require.Equal(t, "foo", str)

		var number int

		require.NoError(t, client.Get(ctx, "b").Scan(&number))
		require.Equal(t, 42, number)

		var flag bool

		require.NoError(t, client.Get(ctx, "c").Scan(&flag))
		require.True(t, flag)

		require.NoError(t, client.Close())
	}
}

func TestRedis_wrongValue(t *testing.T) {
	t.Parallel()

	vs := map[string]any{"a": []string{"b", "c"}}
	p := redis.Preset(redis.WithValues(vs))

	c, err := gnomock.Start(p)

	defer func() { require.NoError(t, gnomock.Stop(c)) }()

	require.Error(t, err)
}
