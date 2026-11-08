package gnomockd

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// collect writes every chunk to a new log writer, waits for each write to be
// consumed, then closes done and returns the collected lines. It fails the
// test instead of hanging when the writer blocks.
func collect(t *testing.T, chunks ...string) []string {
	t.Helper()

	done := make(chan bool)
	w, allLogs := setupLogWriter(done)

	written := make(chan error, 1)

	go func() {
		for _, chunk := range chunks {
			if _, err := io.WriteString(w, chunk); err != nil {
				written <- err
				return
			}
		}

		written <- nil
	}()

	select {
	case err := <-written:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		close(done)
		t.Fatal("log writer blocked")
	}

	close(done)

	select {
	case logs := <-allLogs:
		return logs
	case <-time.After(10 * time.Second):
		t.Fatal("collected logs not delivered")
	}

	return nil
}

func TestSetupLogWriter_lines(t *testing.T) {
	t.Parallel()

	logs := collect(t, "first\nsec", "ond\n\nthird\r\nunterminated")

	require.Equal(t, []string{"first", "second", "", "third", "unterminated"}, logs)
}

func TestSetupLogWriter_noLogs(t *testing.T) {
	t.Parallel()

	require.Empty(t, collect(t))
}

// TestSetupLogWriter_longLine covers the old failure: a line longer than
// bufio.Scanner's 64 KiB limit stopped the scanner, after which the writer
// blocked forever and the collector spun on a closed channel.
func TestSetupLogWriter_longLine(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 3*maxLogLineBytes)

	logs := collect(t, "before\n", long, "\nafter\n", long)

	require.Len(t, logs, 4)
	require.Equal(t, "before", logs[0])
	require.Equal(t, strings.Repeat("x", maxLogLineBytes)+truncatedSuffix, logs[1])
	require.Equal(t, "after", logs[2])
	require.Equal(t, logs[1], logs[3], "unterminated long line")
}

func TestSetupLogWriter_lineAtLimit(t *testing.T) {
	t.Parallel()

	exact := strings.Repeat("y", maxLogLineBytes)

	require.Equal(t, []string{exact}, collect(t, exact+"\n"))
}

func TestSetupLogWriter_boundedRetention(t *testing.T) {
	t.Parallel()

	line := strings.Repeat("z", 1000)

	var b strings.Builder
	for range 3000 {
		b.WriteString(line + "\n")
	}

	b.WriteString("last\n")

	logs := collect(t, b.String())

	require.Regexp(t, `^\[\d+ earlier log lines dropped\]$`, logs[0])
	require.Equal(t, "last", logs[len(logs)-1])

	size := 0
	for _, l := range logs[1:] {
		size += len(l)
	}

	require.LessOrEqual(t, size, maxLogBytes)
	require.Greater(t, size, maxLogBytes-len(line)-1)
}

// TestSetupLogWriter_closedAfterDone checks that writes fail instead of
// blocking once done is closed, including a write already waiting.
func TestSetupLogWriter_closedAfterDone(t *testing.T) {
	t.Parallel()

	done := make(chan bool)
	w, allLogs := setupLogWriter(done)

	close(done)
	<-allLogs

	_, err := io.WriteString(w, "late\n")
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestSetupLogWriter_noGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	collect(t, "a\n", strings.Repeat("b", 2*maxLogLineBytes), "\nc")

	// Closing done without consuming the result must not leak either: the
	// result channel is buffered.
	done := make(chan bool)
	w, _ := setupLogWriter(done)
	_, err := io.WriteString(w, "unread\n")
	require.NoError(t, err)
	close(done)

	require.Eventually(t, func() bool {
		_, err := io.WriteString(w, "x")
		return err != nil
	}, 5*time.Second, 10*time.Millisecond)
}
