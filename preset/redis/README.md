# Gnomock Redis

Gnomock Redis is a [Gnomock](https://github.com/orlangure/gnomock) preset for
running tests against a real Redis container, without mocks.

```go
package redis_test

import (
	"context"
	"fmt"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/redis"
	redisclient "github.com/redis/go-redis/v9"
)

func ExamplePreset() {
	vs := make(map[string]any)

	vs["a"] = "foo"
	vs["b"] = 42
	vs["c"] = true

	p := redis.Preset(redis.WithValues(vs))
	container, _ := gnomock.Start(p)

	defer func() { _ = gnomock.Stop(container) }()

	ctx := context.Background()
	addr := container.DefaultAddress()
	client := redisclient.NewClient(&redisclient.Options{Addr: addr})

	defer func() { _ = client.Close() }()

	fmt.Println(client.Get(ctx, "a").Result())

	var number int

	err := client.Get(ctx, "b").Scan(&number)
	fmt.Println(number, err)

	var flag bool

	err = client.Get(ctx, "c").Scan(&flag)
	fmt.Println(flag, err)

	// Output:
	// foo <nil>
	// 42 <nil>
	// true <nil>
}
```
