package gracefulshutdownserver

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
)

type Database interface {
	Query(ctx context.Context) error
	Shutdown()
}

type database struct {
	pool *ConnPool
}

func NewDatabase(ctx context.Context, addr string, maxConnections int) Database {
	connFactory := func() (net.Conn, error) {
		return net.Dial("tcp", addr)
	}
	d := &database{
		pool: NewPool(ctx, maxConnections, connFactory),
	}
	return d
}

func (d *database) Query(ctx context.Context) error {
	return d.pool.Execute(ctx, func(conn net.Conn) error {
		// doing some stuff
		log.Default().Println("query database")
		return nil
	})
}

func (d *database) Shutdown() {
	log.Default().Println("shutting down database")
	d.pool.Shutdown()
	log.Default().Println("database stopped")
}

type ConnPool struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	factory  func() (net.Conn, error)
	idleCons chan net.Conn
	sema     chan struct{}
}

func NewPool(ctx context.Context, cap int, factory func() (net.Conn, error)) *ConnPool {
	ctx, cancel := context.WithCancel(ctx)

	return &ConnPool{
		ctx:    ctx,
		cancel: cancel,
		wg:     sync.WaitGroup{},

		factory:  factory,
		idleCons: make(chan net.Conn, cap),
		sema:     make(chan struct{}, cap),
	}
}

func (cp *ConnPool) Execute(ctx context.Context, fn func(conn net.Conn) error) error {
	if cp.IsShutdown() {
		return errors.New("already shutdown")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case cp.sema <- struct{}{}:
		defer func() { <-cp.sema }()
	}

	cp.wg.Add(1)
	defer cp.wg.Done()

	conn, err := cp.Get(ctx)
	if err != nil {
		return err
	}
	defer cp.Put(conn)
	return fn(conn)
}

func (cp *ConnPool) Get(ctx context.Context) (net.Conn, error) {
	if cp.IsShutdown() {
		return nil, errors.New("already shutdown")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case conn := <-cp.idleCons:
		return conn, nil
	default:
		return cp.factory()
	}

}

func (cp *ConnPool) Put(conn net.Conn) {
	if cp.IsShutdown() {
		_ = conn.Close()
		return
	}
	select {
	case cp.idleCons <- conn:
	default:
		_ = conn.Close()
	}
}

func (cp *ConnPool) Shutdown() {
	if cp.IsShutdown() {
		return
	}
	cp.cancel()
	cp.wg.Wait()

	close(cp.idleCons)
	for conn := range cp.idleCons {
		_ = conn.Close()
	}
}

func (cp *ConnPool) IsShutdown() bool {
	return cp.ctx.Err() != nil
}
