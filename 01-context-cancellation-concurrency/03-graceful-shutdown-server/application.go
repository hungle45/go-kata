package gracefulshutdownserver

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Application struct {
	httpServer *HttpServer
	pool       WorkerPool[Data]
	cache      Cache
	db         Database

	ctx    context.Context
	cancel context.CancelFunc

	srvAddr string
	dbAddr  string

	shutdownTimeout time.Duration
}

func InitApplication(srvAddr, dbAddr string) *Application {
	ctx, cancel := context.WithCancel(context.Background())
	db := NewDatabase(ctx, dbAddr, 10)
	cache := NewCache(ctx, 30*time.Second)
	pool := NewWorkerPool[Data](ctx, 10)
	controller := NewController(pool, cache, db)
	httpServer := NewHttpServer(srvAddr, controller)

	return &Application{
		httpServer:      httpServer,
		pool:            pool,
		cache:           cache,
		db:              db,
		ctx:             ctx,
		cancel:          cancel,
		srvAddr:         srvAddr,
		dbAddr:          dbAddr,
		shutdownTimeout: 10 * time.Second,
	}
}

func (app *Application) Start() {
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go app.httpServer.Start()

	<-stop
	log.Println("Shutdown signal received, starting graceful shutdown...")
	app.cancel()

	timeout := app.shutdownTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), timeout)
	defer shutdownCancel()

	app.Shutdown(shutdownCtx)
}

func (app *Application) Shutdown(ctx context.Context) {
	log.Println("Shutting down application components...")

	app.httpServer.Shutdown(ctx)
	if app.pool != nil {
		app.pool.Shutdown()
	}
	if app.cache != nil {
		app.cache.Shutdown()
	}
	if app.db != nil {
		app.db.Shutdown()
	}

	log.Println("Application shutdown complete")
}
