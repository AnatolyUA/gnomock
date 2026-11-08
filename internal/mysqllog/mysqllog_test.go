package mysqllog_test

import (
	"sync"
	"testing"

	"github.com/orlangure/gnomock/internal/mysqllog"
)

// TestDiscard_concurrent fails under -race if concurrent callers can reach
// the driver's SetLogger more than once.
func TestDiscard_concurrent(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(mysqllog.Discard)
	}

	wg.Wait()
}
