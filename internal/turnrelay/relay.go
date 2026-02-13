package turnrelay

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
)

// Relay runs the TURN relay: DCC front-end and bot-facing TLS.
type Relay struct {
	config       *RelayConfig
	sessions     map[string]*Session
	sessionsMu   sync.RWMutex
	portPool     *portPool
	currentConns int32
	maxSessions  int
}

// RelayConfig is the relay configuration used by turnrelay.
type RelayConfig struct {
	TURNListen  string
	TURNSecret  string
	DCCPortMin  int
	DCCPortMax  int
	RelayHost   string
	TLSCertFile string
	TLSKeyFile  string
	MaxSessions int
}

func NewRelay(c *RelayConfig) (*Relay, error) {
	pool, err := newPortPool(c.DCCPortMin, c.DCCPortMax)
	if err != nil {
		return nil, err
	}
	maxSessions := c.MaxSessions
	if maxSessions <= 0 {
		maxSessions = 100
	}
	return &Relay{
		config:      c,
		sessions:    make(map[string]*Session),
		portPool:    pool,
		maxSessions: maxSessions,
	}, nil
}

func (r *Relay) Run() error {
	tlsConfig, err := r.tlsConfig()
	if err != nil {
		return err
	}
	turnLn, err := tls.Listen("tcp", r.config.TURNListen, tlsConfig)
	if err != nil {
		return fmt.Errorf("turns listen: %w", err)
	}
	go r.acceptBotConnections(turnLn)
	go r.acceptDCCConnections(tlsConfig)
	log.Printf("relay: TURN listening on %s", r.config.TURNListen)
	return nil
}

func (r *Relay) tlsConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(r.config.TLSCertFile, r.config.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

func (r *Relay) acceptBotConnections(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("relay: accept bot: %v", err)
			return
		}
		go r.handleBotConnection(conn.(*tls.Conn))
	}
}

func (r *Relay) acceptDCCConnections(tlsConfig *tls.Config) {
	_ = tlsConfig
	select {}
}

func (r *Relay) handleBotConnection(conn *tls.Conn) {
	defer conn.Close()
	if n := atomic.AddInt32(&r.currentConns, 1); n > int32(r.maxSessions) {
		atomic.AddInt32(&r.currentConns, -1)
		return
	}
	defer atomic.AddInt32(&r.currentConns, -1)

	for {
		msgType, payload, err := ReadFrame(conn)
		if err != nil {
			if err != io.EOF {
				log.Printf("relay: bot frame read: %v", err)
			}
			return
		}
		switch msgType {
		case MsgRegisterDownload:
			if len(payload) < 4 {
				_ = WriteFrame(conn, MsgError, []byte("bad RegisterDownload"))
				continue
			}
			sessionID := string(payload[:min(36, len(payload))])
			filename := ""
			if len(payload) > 36 {
				filename = string(payload[36:])
			}
			port, err := r.allocateDCCPort(sessionID, "download", filename)
			if err != nil {
				_ = WriteFrame(conn, MsgError, []byte(err.Error()))
				continue
			}
			resp := make([]byte, 4)
			binary.BigEndian.PutUint32(resp, uint32(port))
			if err := WriteFrame(conn, MsgPortAlloc, resp); err != nil {
				return
			}
			r.relayDownloadToUser(conn, sessionID)
			return
		case MsgRegisterUpload:
			if len(payload) < 4 {
				_ = WriteFrame(conn, MsgError, []byte("bad RegisterUpload"))
				continue
			}
			sessionID := string(payload[:min(36, len(payload))])
			filename := ""
			if len(payload) > 36 {
				filename = string(payload[36:])
			}
			port, err := r.allocateDCCPort(sessionID, "upload", filename)
			if err != nil {
				_ = WriteFrame(conn, MsgError, []byte(err.Error()))
				continue
			}
			resp := make([]byte, 4)
			binary.BigEndian.PutUint32(resp, uint32(port))
			if err := WriteFrame(conn, MsgPortAlloc, resp); err != nil {
				return
			}
			r.relayUploadFromUser(conn, sessionID)
			return
		default:
			_ = WriteFrame(conn, MsgError, []byte("unknown message type"))
			return
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (r *Relay) allocateDCCPort(sessionID, kind, filename string) (int, error) {
	port, err := r.portPool.allocate()
	if err != nil {
		return 0, err
	}
	sess := NewSession(sessionID, kind, filename, port)
	r.sessionsMu.Lock()
	r.sessions[sessionID] = sess
	r.sessionsMu.Unlock()
	tlsConfig, _ := r.tlsConfig()
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", port), tlsConfig)
	if err != nil {
		r.portPool.release(port)
		r.sessionsMu.Lock()
		delete(r.sessions, sessionID)
		r.sessionsMu.Unlock()
		return 0, err
	}
	go r.listenDCCForSession(ln, sessionID)
	return port, nil
}

func (r *Relay) listenDCCForSession(ln net.Listener, sessionID string) {
	defer ln.Close()
	conn, err := ln.Accept()
	if err != nil {
		r.removeSession(sessionID)
		return
	}
	defer conn.Close()
	r.sessionsMu.RLock()
	sess, ok := r.sessions[sessionID]
	r.sessionsMu.RUnlock()
	if !ok {
		return
	}
	if sess.Kind == "download" {
		_, _ = io.Copy(conn, &ChanReader{Ch: sess.BotStream})
		sess.Close()
	} else {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				select {
				case sess.UserConn <- buf[:n:n]:
				case <-sess.Done:
					return
				}
			}
			if err != nil {
				close(sess.UserConn)
				sess.Close()
				return
			}
		}
	}
}

func (r *Relay) relayDownloadToUser(botConn *tls.Conn, sessionID string) {
	r.sessionsMu.RLock()
	sess, ok := r.sessions[sessionID]
	r.sessionsMu.RUnlock()
	if !ok {
		return
	}
	for {
		msgType, payload, err := ReadFrame(botConn)
		if err != nil {
			sess.Close()
			return
		}
		switch msgType {
		case MsgData:
			select {
			case sess.BotStream <- payload:
			case <-sess.Done:
				return
			}
		case MsgEOF:
			close(sess.BotStream)
			sess.Close()
			return
		default:
			sess.Close()
			return
		}
	}
}

func (r *Relay) relayUploadFromUser(botConn *tls.Conn, sessionID string) {
	r.sessionsMu.RLock()
	sess, ok := r.sessions[sessionID]
	r.sessionsMu.RUnlock()
	if !ok {
		return
	}
	for {
		select {
		case data, ok := <-sess.UserConn:
			if !ok {
				_ = WriteFrame(botConn, MsgEOF, nil)
				r.removeSession(sessionID)
				return
			}
			if err := WriteFrame(botConn, MsgData, data); err != nil {
				r.removeSession(sessionID)
				return
			}
		case <-sess.Done:
			r.removeSession(sessionID)
			return
		}
	}
}

func (r *Relay) removeSession(sessionID string) {
	r.sessionsMu.Lock()
	sess, ok := r.sessions[sessionID]
	delete(r.sessions, sessionID)
	r.sessionsMu.Unlock()
	if ok {
		sess.Close()
		if sess.Port > 0 {
			r.portPool.release(sess.Port)
		}
	}
}

type portPool struct {
	min, max int
	used     map[int]bool
	mu       sync.Mutex
}

func newPortPool(minPort, maxPort int) (*portPool, error) {
	if minPort <= 0 || maxPort < minPort {
		return nil, fmt.Errorf("invalid port range %d-%d", minPort, maxPort)
	}
	return &portPool{
		min:  minPort,
		max:  maxPort,
		used: make(map[int]bool),
	}, nil
}

func (p *portPool) allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b := make([]byte, 2)
	for i := 0; i < 100; i++ {
		if _, err := rand.Read(b); err != nil {
			return 0, err
		}
		port := p.min + (int(binary.BigEndian.Uint16(b)) % (p.max - p.min + 1))
		if !p.used[port] {
			p.used[port] = true
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port in %d-%d", p.min, p.max)
}

func (p *portPool) release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.used, port)
}
