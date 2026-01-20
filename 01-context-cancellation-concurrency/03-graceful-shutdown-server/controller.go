package gracefulshutdownserver

import (
	"context"
	"log"
	"net/http"
)

type Data string
type Controller struct {
	pool  WorkerPool[Data]
	cache Cache
	db    Database
}

func NewController(pool WorkerPool[Data], cache Cache, db Database) *Controller {
	return &Controller{
		pool:  pool,
		cache: cache,
		db:    db,
	}
}

func (h *Controller) SetupRouter(srv *http.ServeMux) {
	srv.HandleFunc("/ping", h.handlerRequest)
}

func (h *Controller) handlerRequest(rw http.ResponseWriter, r *http.Request) {
	f := h.pool.Submit(r.Context(), func(ctx context.Context) (Data, error) {
		log.Default().Println("handle request")
		err := h.db.Query(ctx)
		if err != nil {
			return "", err
		}
		return "pong", nil
	})

	data, err := f.Get()
	if err != nil {
		log.Println("handle request error: ", err)
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	rw.WriteHeader(http.StatusOK)
	_, err = rw.Write([]byte(data))
	if err != nil {
		log.Println("handle request error: ", err)
		return
	}
}
