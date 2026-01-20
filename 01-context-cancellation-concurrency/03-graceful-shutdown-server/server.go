package gracefulshutdownserver

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

type HttpServer struct {
	server     *http.Server
	controller *Controller
}

func (s *HttpServer) Start() {
	log.Default().Println("starting http server at address", s.server.Addr)
	err := s.server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Default().Println("http server stopped with error", err.Error())
	}
	log.Default().Println("http server stopped")
}

func (s *HttpServer) Shutdown(ctx context.Context) {
	log.Default().Println("shutting down http server")
	err := s.server.Shutdown(ctx)
	if err != nil {
		log.Default().Println("error shutting down http server", err.Error())
		return
	}
}

func (s *HttpServer) UpdateController(controller *Controller) {
	s.controller = controller
	mux := http.NewServeMux()
	controller.SetupRouter(mux)
	s.server.Handler = mux
}

func NewHttpServer(address string, controller *Controller) *HttpServer {
	mux := http.NewServeMux()
	if controller != nil {
		controller.SetupRouter(mux)
	}

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       10 * time.Second, // Added: prevent slow client attacks
		WriteTimeout:      10 * time.Second, // Added: prevent slow writes
		IdleTimeout:       30 * time.Second, // Added: close idle connections
	}

	return &HttpServer{
		server:     server,
		controller: controller,
	}
}
