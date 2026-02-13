package turnrelay

import (
	"io"
	"sync"
	"time"
)

// Session represents a single download or upload session.
type Session struct {
	ID        string
	Kind      string
	Filename  string
	CreatedAt time.Time
	UserConn  chan []byte
	BotStream chan []byte
	Done      chan struct{}
	Port      int
	mu        sync.Mutex
}

// NewSession creates a session.
func NewSession(id, kind, filename string, port int) *Session {
	return &Session{
		ID:        id,
		Kind:      kind,
		Filename:  filename,
		CreatedAt: time.Now(),
		Port:      port,
		UserConn:  make(chan []byte, 256),
		BotStream: make(chan []byte, 512),
		Done:      make(chan struct{}),
	}
}

// ChanReader implements io.Reader by reading from a channel of byte slices.
type ChanReader struct {
	Ch   <-chan []byte
	cur  []byte
	done bool
}

func (c *ChanReader) Read(p []byte) (n int, err error) {
	for len(c.cur) == 0 && !c.done {
		data, ok := <-c.Ch
		if !ok {
			c.done = true
			return 0, io.EOF
		}
		c.cur = data
	}
	if len(c.cur) == 0 {
		return 0, io.EOF
	}
	n = copy(p, c.cur)
	c.cur = c.cur[n:]
	return n, nil
}

func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.Done:
	default:
		close(s.Done)
	}
}
