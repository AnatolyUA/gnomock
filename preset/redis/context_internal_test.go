package redis

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/orlangure/gnomock"
	"github.com/stretchr/testify/require"
)

// TestHealthcheck_honorsContext runs the healthcheck against a server that
// accepts connections but never replies. The healthcheck must return when its
// context ends, not when the driver's own 3s read timeout fires.
func TestHealthcheck_honorsContext(t *testing.T) {
	t.Parallel()

	c := silentServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := healthcheck(ctx, c)

	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
}

// TestValuesInit_honorsContext does the same for the WithValues init.
func TestValuesInit_honorsContext(t *testing.T) {
	t.Parallel()

	c := silentServer(t)

	p := &P{Values: map[string]any{"key": "value"}}

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	start := time.Now()
	err := p.initf(ctx, c)

	require.Error(t, err)
	require.Less(t, time.Since(start), 2*time.Second)
}

// silentServer starts a TCP server that accepts connections and never
// writes, and returns a container pointing to it.
func silentServer(t *testing.T) *gnomock.Container {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var (
		mu    sync.Mutex
		conns []net.Conn
		wg    sync.WaitGroup
	)

	wg.Go(func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}

			mu.Lock()

			conns = append(conns, conn)
			mu.Unlock()
		}
	})

	t.Cleanup(func() {
		_ = l.Close()

		wg.Wait()

		mu.Lock()
		defer mu.Unlock()

		for _, conn := range conns {
			_ = conn.Close()
		}
	})

	addr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok)

	return &gnomock.Container{
		Host: addr.IP.String(),
		Ports: gnomock.NamedPorts{
			gnomock.DefaultPort: {Protocol: "tcp", Port: addr.Port},
		},
	}
}
