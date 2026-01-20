package gracefulshutdownserver

import (
	"context"
	"log"
	"time"
)

type Cache interface {
	Shutdown()
}

type cache struct {
	ctx             context.Context
	cancel          context.CancelFunc
	refreshInterval time.Duration
}

func NewCache(parentCtx context.Context, refreshInterval time.Duration) Cache {
	ctx, cancel := context.WithCancel(parentCtx)
	c := &cache{
		ctx:             ctx,
		cancel:          cancel,
		refreshInterval: refreshInterval,
	}
	go c.pooling()
	return c
}

func (c *cache) Shutdown() {
	log.Default().Println("shutting down cache")
	c.cancel()
	log.Default().Println("cache stopped")
}

func (c *cache) pooling() {
	c.refresh()
	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.refresh()
		case <-c.ctx.Done():
			log.Default().Println("stopping cache refresh...")
			return
		}
	}

}

func (c *cache) refresh() {
	log.Default().Println("refreshing cache...")
}
