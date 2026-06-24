package agent

import (
	"bufio"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/makeding/hiraku/internal/config"
	"github.com/makeding/hiraku/internal/protocol"
)

type Server struct {
	cfg     config.Config
	manager *Manager
	logger  *log.Logger
}

func NewServer(cfg config.Config, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return &Server{
		cfg:     cfg,
		manager: NewManager(cfg),
		logger:  logger,
	}
}

func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return err
	}
	defer ln.Close()

	s.logger.Printf("listening on %s", s.cfg.Listen)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	defer conn.Close()

	if !s.cfg.AllowsRemoteAddr(conn.RemoteAddr().String()) {
		s.logger.Printf("rejecting connection from %s: remote address is not allowed", conn.RemoteAddr())
		s.writeError(conn, errors.New("remote address is not allowed"))
		return
	}

	br := bufio.NewReader(conn)
	req, err := protocol.ReadRequest(br)
	if err != nil {
		s.writeError(conn, err)
		return
	}
	if req.Secret != s.cfg.Secret {
		s.writeError(conn, errors.New("authentication failed"))
		return
	}

	consumer, err := s.manager.Acquire(req.Mode, req.Channel)
	if err != nil {
		s.writeError(conn, err)
		return
	}

	if err := protocol.WriteResponse(conn, protocol.Response{OK: true}); err != nil {
		consumer.Release()
		return
	}

	_ = consumer.CopyTo(conn)
	consumer.ReleaseAfter(time.Duration(s.cfg.DisconnectCloseDelaySeconds) * time.Second)
}

func (s *Server) writeError(w io.Writer, err error) {
	_ = protocol.WriteResponse(w, protocol.Response{
		OK:    false,
		Error: err.Error(),
	})
}
