package server

import (
	"errors"
	"io"
	"os"
	"sync"
)

// concatReader presents several files as one continuous, seekable stream. It
// is what lets a multi-file audiobook be served as a single podcast episode:
// http.ServeContent can then answer Range requests against the whole book.
//
// Only formats whose frames can simply be concatenated (MPEG audio) should be
// used with it.
type concatReader struct {
	mu     sync.Mutex
	paths  []string
	sizes  []int64
	starts []int64
	total  int64

	cur int // index of the open file, -1 when nothing is open
	f   *os.File
	pos int64
}

var errEmptyStream = errors.New("stream: no readable audio files")

func newConcatReader(paths []string) (*concatReader, error) {
	c := &concatReader{cur: -1}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || st.Size() == 0 {
			continue
		}
		c.paths = append(c.paths, p)
		c.sizes = append(c.sizes, st.Size())
		c.starts = append(c.starts, c.total)
		c.total += st.Size()
	}
	if len(c.paths) == 0 {
		return nil, errEmptyStream
	}
	return c, nil
}

func (c *concatReader) Size() int64 { return c.total }

func (c *concatReader) indexAt(pos int64) int {
	lo, hi := 0, len(c.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if c.starts[mid] <= pos {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

func (c *concatReader) openAt(i int) error {
	if c.f != nil {
		_ = c.f.Close()
		c.f = nil
	}
	if i < 0 || i >= len(c.paths) {
		return io.EOF
	}
	f, err := os.Open(c.paths[i])
	if err != nil {
		return err
	}
	c.f, c.cur = f, i
	return nil
}

func (c *concatReader) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	// Two attempts are enough: at most one zero-length/truncated file can be
	// skipped before a real read happens.
	for attempt := 0; attempt < 2; attempt++ {
		if c.pos >= c.total {
			return 0, io.EOF
		}
		i := c.indexAt(c.pos)
		if c.f == nil || c.cur != i {
			if err := c.openAt(i); err != nil {
				return 0, err
			}
		}
		fileEnd := c.starts[i] + c.sizes[i]
		remaining := fileEnd - c.pos
		if remaining <= 0 {
			c.pos = fileEnd
			continue
		}
		chunk := p
		if int64(len(chunk)) > remaining {
			chunk = chunk[:remaining]
		}
		n, err := c.f.ReadAt(chunk, c.pos-c.starts[i])
		c.pos += int64(n)
		if n > 0 {
			if errors.Is(err, io.EOF) && c.pos < c.total {
				err = nil
			}
			return n, err
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		// Short file: jump to the next one.
		c.pos = fileEnd
	}
	return 0, io.EOF
}

func (c *concatReader) Seek(offset int64, whence int) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = c.pos + offset
	case io.SeekEnd:
		abs = c.total + offset
	default:
		return 0, errors.New("stream: invalid whence")
	}
	if abs < 0 {
		return 0, errors.New("stream: negative position")
	}
	if abs > c.total {
		abs = c.total
	}
	c.pos = abs
	return abs, nil
}

func (c *concatReader) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.f != nil {
		err := c.f.Close()
		c.f, c.cur = nil, -1
		return err
	}
	return nil
}
