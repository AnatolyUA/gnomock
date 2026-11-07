package gnomock

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// stdFrame builds a single frame of a multiplexed docker log stream: an
// 8-byte header (stream type, 3 zero bytes, big-endian payload size) followed
// by the payload. The stdcopy package that gnomock.go uses only exposes the
// reader side (StdCopy), so the writer side is implemented here following the
// documented wire format.
func stdFrame(stream stdcopy.StdType, payload string) []byte {
	frame := make([]byte, 8, 8+len(payload))
	frame[0] = byte(stream)
	binary.BigEndian.PutUint32(frame[4:], uint32(len(payload)))

	return append(frame, payload...)
}

// errReader returns the configured error on every Read call.
type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

// fakeReadCloser is an io.ReadCloser whose Close returns the configured error.
type fakeReadCloser struct {
	io.Reader

	closeErr error
	closed   bool
}

func (f *fakeReadCloser) Close() error {
	f.closed = true

	return f.closeErr
}

// eofNotifyReader closes eof once the wrapped reader returns io.EOF.
type eofNotifyReader struct {
	io.ReadCloser

	once sync.Once
	eof  chan struct{}
}

func (r *eofNotifyReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if errors.Is(err, io.EOF) {
		r.once.Do(func() { close(r.eof) })
	}

	return n, err
}

// syncBuffer is a bytes.Buffer safe for concurrent use.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestCopyf_multiplexedStream(t *testing.T) {
	t.Parallel()

	var src bytes.Buffer

	src.Write(stdFrame(stdcopy.Stdout, "stdout line 1\n"))
	src.Write(stdFrame(stdcopy.Stderr, "stderr line 1\n"))
	src.Write(stdFrame(stdcopy.Stdout, "stdout line 2\n"))
	src.Write(stdFrame(stdcopy.Stderr, "stderr line 2\n"))

	var dst bytes.Buffer

	require.NoError(t, copyf(&dst, &src)())
	require.Equal(
		t,
		"stdout line 1\nstderr line 1\nstdout line 2\nstderr line 2\n",
		dst.String(),
	)
}

func TestCopyf_largeFrame(t *testing.T) {
	t.Parallel()

	// A frame larger than the initial 32KiB stdcopy buffer.
	payload := string(bytes.Repeat([]byte("x"), 100*1024))

	var src bytes.Buffer

	src.Write(stdFrame(stdcopy.Stdout, payload))
	src.Write(stdFrame(stdcopy.Stderr, "end"))

	var dst bytes.Buffer

	require.NoError(t, copyf(&dst, &src)())
	require.Equal(t, payload+"end", dst.String())
}

func TestCopyf_errors(t *testing.T) {
	t.Parallel()

	errOther := errors.New("other error")

	tests := []struct {
		name    string
		src     io.Reader
		wantErr error
		wantMsg string
		wantDst string
	}{
		{
			name: "empty stream",
			src:  &bytes.Buffer{},
		},
		{
			name: "immediate io.EOF",
			src:  errReader{err: io.EOF},
		},
		{
			name: "wrapped io.EOF",
			src:  errReader{err: fmt.Errorf("read: %w", io.EOF)},
		},
		{
			name: "net.ErrClosed",
			src:  errReader{err: net.ErrClosed},
		},
		{
			name: "wrapped net.ErrClosed",
			src:  errReader{err: fmt.Errorf("read tcp: %w", net.ErrClosed)},
		},
		{
			name:    "other error is returned",
			src:     errReader{err: errOther},
			wantErr: errOther,
		},
		{
			// Current behavior: a stream that is not multiplexed fails on
			// the first byte, which is interpreted as an unknown stream type
			// ('p' == 112).
			name:    "non-multiplexed stream",
			src:     bytes.NewBufferString("plain text log line\n"),
			wantMsg: "unrecognized stream: 112",
		},
		{
			// Current behavior: input shorter than a frame header is
			// silently ignored.
			name: "input shorter than a header",
			src:  bytes.NewBufferString("abc"),
		},
		{
			// Current behavior: a truncated last frame is silently dropped,
			// complete frames before it are written.
			name: "truncated frame",
			src: io.MultiReader(
				bytes.NewReader(stdFrame(stdcopy.Stdout, "complete")),
				bytes.NewReader(stdFrame(stdcopy.Stdout, "truncated")[:12]),
			),
			wantDst: "complete",
		},
		{
			name: "daemon error frame",
			src: io.MultiReader(
				bytes.NewReader(stdFrame(stdcopy.Stdout, "before")),
				bytes.NewReader(stdFrame(stdcopy.Systemerr, "boom")),
				bytes.NewReader(stdFrame(stdcopy.Stdout, "after")),
			),
			wantMsg: "error from daemon in stream: boom",
			wantDst: "before",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var dst bytes.Buffer

			err := copyf(&dst, tt.src)()

			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantMsg != "":
				require.EqualError(t, err, tt.wantMsg)
			default:
				require.NoError(t, err)
			}

			require.Equal(t, tt.wantDst, dst.String())
		})
	}
}

func TestCloseLogReader(t *testing.T) {
	t.Parallel()

	errClose := errors.New("close error")
	errTask := errors.New("task error")

	tests := []struct {
		name     string
		closeErr error
		taskErr  error
		wantErr  error
		wantWait bool
	}{
		{
			name:     "no errors",
			wantWait: true,
		},
		{
			name:     "net.ErrClosed on close is ignored",
			closeErr: net.ErrClosed,
			wantWait: true,
		},
		{
			name:     "wrapped net.ErrClosed on close is ignored",
			closeErr: fmt.Errorf("close tcp: %w", net.ErrClosed),
			wantWait: true,
		},
		{
			name:     "other close error is returned without waiting",
			closeErr: errClose,
			wantErr:  errClose,
			wantWait: false,
		},
		{
			name:     "task error is propagated after close",
			taskErr:  errTask,
			wantErr:  errTask,
			wantWait: true,
		},
		{
			name:     "task error is propagated after ignored close error",
			closeErr: net.ErrClosed,
			taskErr:  errTask,
			wantErr:  errTask,
			wantWait: true,
		},
		{
			name:     "close error takes precedence over task error",
			closeErr: errClose,
			taskErr:  errTask,
			wantErr:  errClose,
			wantWait: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rc := &fakeReadCloser{Reader: &bytes.Buffer{}, closeErr: tt.closeErr}

			// The task blocks until released, so that closeLogReader waiting
			// (or not waiting) for it is observable.
			release := make(chan struct{})
			finished := make(chan struct{})

			eg := &errgroup.Group{}
			eg.Go(func() error {
				defer close(finished)

				<-release

				return tt.taskErr
			})

			done := make(chan error, 1)

			go func() {
				done <- closeLogReader(rc, eg)()
			}()

			if tt.wantWait {
				select {
				case err := <-done:
					t.Fatalf("closeLogReader returned before the task finished: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
			}

			close(release)

			err := <-done

			<-finished

			require.True(t, rc.closed)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLogForwarding_pipe mimics setupLogForwarding with an io.Pipe as the log
// source.
func TestLogForwarding_pipe(t *testing.T) {
	t.Parallel()

	t.Run("writer finishes before close", func(t *testing.T) {
		t.Parallel()

		pr, pw := io.Pipe()
		r := &eofNotifyReader{ReadCloser: pr, eof: make(chan struct{})}
		dst := &syncBuffer{}

		eg := &errgroup.Group{}
		eg.Go(copyf(dst, r))

		_, err := pw.Write(stdFrame(stdcopy.Stdout, "out\n"))
		require.NoError(t, err)
		_, err = pw.Write(stdFrame(stdcopy.Stderr, "err\n"))
		require.NoError(t, err)
		require.NoError(t, pw.Close())

		// The stream ends (io.EOF) before the reader is closed, as when a
		// container exits before Stop.
		<-r.eof

		require.NoError(t, closeLogReader(r, eg)())
		require.Equal(t, "out\nerr\n", dst.String())
	})

	t.Run("reader closed while copying", func(t *testing.T) {
		t.Parallel()

		pr, pw := io.Pipe()
		dst := &syncBuffer{}

		eg := &errgroup.Group{}
		eg.Go(copyf(dst, pr))

		_, err := pw.Write(stdFrame(stdcopy.Stdout, "out\n"))
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return dst.String() == "out\n"
		}, 5*time.Second, 10*time.Millisecond)

		// Current behavior: closing an io.PipeReader makes the pending Read
		// fail with io.ErrClosedPipe, which is neither io.EOF nor
		// net.ErrClosed, so it is propagated by copyf and closeLogReader.
		err = closeLogReader(pr, eg)()
		require.ErrorIs(t, err, io.ErrClosedPipe)
		require.Equal(t, "out\n", dst.String())

		_, err = pw.Write(stdFrame(stdcopy.Stdout, "late\n"))
		require.ErrorIs(t, err, io.ErrClosedPipe)
	})
}

// TestLogForwarding_tcp mimics setupLogForwarding with a network connection
// as the log source: closing the connection while copyf is blocked in Read
// returns net.ErrClosed, which must not be reported as an error.
func TestLogForwarding_tcp(t *testing.T) {
	t.Parallel()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	serverDone := make(chan error, 1)

	go func() {
		serverDone <- func() error {
			conn, err := l.Accept()
			if err != nil {
				return err
			}

			defer func() { _ = conn.Close() }()

			if _, err := conn.Write(stdFrame(stdcopy.Stdout, "out\n")); err != nil {
				return err
			}

			if _, err := conn.Write(stdFrame(stdcopy.Stderr, "err\n")); err != nil {
				return err
			}

			// Keep the connection open until the client closes it.
			_, err = io.Copy(io.Discard, conn)

			return err
		}()
	}()

	conn, err := net.Dial("tcp", l.Addr().String())
	require.NoError(t, err)

	dst := &syncBuffer{}

	eg := &errgroup.Group{}
	eg.Go(copyf(dst, conn))

	require.Eventually(t, func() bool {
		return dst.String() == "out\nerr\n"
	}, 5*time.Second, 10*time.Millisecond)

	require.NoError(t, closeLogReader(conn, eg)())
	require.Equal(t, "out\nerr\n", dst.String())

	require.NoError(t, <-serverDone)
	require.NoError(t, l.Close())
}
