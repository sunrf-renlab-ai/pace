package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
)

type HandlerFunc func(req Request) Response

type Server struct {
	handlers map[string]HandlerFunc
	listener net.Listener
	mu       sync.Mutex
}

func socketPath() (string, error) {
	if p := os.Getenv("MENTOR_SOCKET_PATH"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "mentor", "sock"), nil
}

func NewServer() (*Server, error) {
	p, err := socketPath()
	if err != nil {
		return nil, err
	}
	os.Remove(p)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", p)
	if err != nil {
		return nil, err
	}
	os.Chmod(p, 0o600)
	return &Server{handlers: map[string]HandlerFunc{}, listener: ln}, nil
}

func (s *Server) Handle(method string, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

func (s *Server) Serve() {
	for {
		c, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *Server) Close() error {
	p, _ := socketPath()
	defer os.Remove(p)
	return s.listener.Close()
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	enc := json.NewEncoder(c)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			enc.Encode(Response{OK: false, Error: "bad json"})
			continue
		}
		s.mu.Lock()
		h, ok := s.handlers[req.Method]
		s.mu.Unlock()
		if !ok {
			enc.Encode(Response{OK: false, Error: "unknown method: " + req.Method})
			continue
		}
		enc.Encode(h(req))
	}
}
