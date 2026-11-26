package http

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

type Server struct {
	server   *http.Server
	listener net.Listener
}

func Listen(port int, routes http.Handler) (*Server, error) {
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, err
	}
	server := &http.Server{
		// Accept only the loopback origin to reject DNS rebinding requests.
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Host != listener.Addr().String() {
				http.Error(w, "Invalid host", http.StatusForbidden)
				return
			}
			routes.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	return &Server{server: server, listener: listener}, nil
}

func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

func (s *Server) Serve() error {
	return s.server.Serve(s.listener)
}

func (s *Server) Shutdown() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		_ = s.server.Close()
		return err
	}
	return nil
}
