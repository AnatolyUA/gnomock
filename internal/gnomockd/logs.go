package gnomockd

import (
	"bufio"
	"fmt"
	"io"
	"sync"
)

const (
	// maxLogLineBytes is the longest log line kept; the rest of a longer line
	// is read and discarded.
	maxLogLineBytes = 64 * 1024

	// maxLogBytes bounds the log lines kept for a start error response. When
	// it is exceeded, the oldest lines are dropped.
	maxLogBytes = 1024 * 1024

	truncatedSuffix = " [line truncated]"
)

// setupLogWriter returns a writer for container logs, and a channel that
// receives the collected log lines once done is closed.
//
// The writer never blocks for long: everything written to it is read right
// away, also after a line exceeds maxLogLineBytes or the kept lines exceed
// maxLogBytes, until done is closed. After that, writes fail with
// io.ErrClosedPipe. This matters because gnomock waits for the log copier
// when it stops a container that failed to start, before done is closed.
func setupLogWriter(done <-chan bool) (io.Writer, chan []string) {
	logReader, logWriter := io.Pipe()
	allLogs := make(chan []string, 1)
	collector := &logCollector{}
	finished := make(chan struct{})

	go func() {
		defer close(finished)

		collector.readLines(logReader)
	}()

	go func() {
		<-done

		// Unblocks the reader goroutine and any pending write.
		_ = logReader.Close()

		<-finished

		allLogs <- collector.result()

		close(allLogs)
	}()

	return logWriter, allLogs
}

// logCollector keeps the most recent log lines within maxLogBytes.
type logCollector struct {
	mu      sync.Mutex
	lines   []string
	size    int
	dropped int
}

// readLines reads r until it fails, and adds every line to c. Lines longer
// than maxLogLineBytes are truncated.
func (c *logCollector) readLines(r io.Reader) {
	br := bufio.NewReader(r)

	var (
		line      []byte
		truncated bool
	)

	for {
		fragment, isPrefix, err := br.ReadLine()
		if err != nil {
			// A partial line is kept when the stream ends without a newline.
			if len(line) > 0 || truncated {
				c.add(line, truncated)
			}

			return
		}

		if room := maxLogLineBytes - len(line); len(fragment) > room {
			line = append(line, fragment[:room]...)
			truncated = true
		} else {
			line = append(line, fragment...)
		}

		if !isPrefix {
			c.add(line, truncated)

			line, truncated = line[:0], false
		}
	}
}

func (c *logCollector) add(line []byte, truncated bool) {
	s := string(line)
	if truncated {
		s += truncatedSuffix
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.lines = append(c.lines, s)
	c.size += len(s)

	for c.size > maxLogBytes && len(c.lines) > 1 {
		c.size -= len(c.lines[0])
		c.lines[0] = ""
		c.lines = c.lines[1:]
		c.dropped++
	}
}

// result returns the kept lines, preceded by a note when older lines were
// dropped.
func (c *logCollector) result() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	lines := make([]string, 0, len(c.lines)+1)

	if c.dropped > 0 {
		lines = append(lines, fmt.Sprintf("[%d earlier log lines dropped]", c.dropped))
	}

	return append(lines, c.lines...)
}
