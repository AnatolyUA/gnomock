package cockroachdb

import (
	"context"
	"net"
	"testing"

	"github.com/orlangure/gnomock"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// TestConnect_failureClosesPool checks that a failed connection attempt
// returns no pool and leaves no database/sql goroutine behind. Init returns
// right away on a connect error, so a pool returned with the error would
// leak its opener goroutine. No Docker needed: the address refuses
// connections, and the second case uses an expired context.
func TestConnect_failureClosesPool(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	addr := refusedAddr(t)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, ctx := range map[string]context.Context{
		"connection refused": context.Background(),
		"canceled context":   canceled,
	} {
		t.Run(name, func(t *testing.T) {
			db, err := connect(ctx, testContainer(addr), "d")
			require.Error(t, err)
			require.Nil(t, db)
		})
	}
}

// refusedAddr returns a local TCP address that nothing listens on.
func refusedAddr(t *testing.T) *net.TCPAddr {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	addr, ok := l.Addr().(*net.TCPAddr)
	require.True(t, ok)
	require.NoError(t, l.Close())

	return addr
}

func testContainer(addr *net.TCPAddr) *gnomock.Container {
	return &gnomock.Container{
		Host: addr.IP.String(),
		Ports: gnomock.NamedPorts{
			gnomock.DefaultPort: {Protocol: "tcp", Port: addr.Port},
		},
	}
}
