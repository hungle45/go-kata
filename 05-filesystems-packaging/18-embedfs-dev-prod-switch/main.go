package main

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"time"
)

func main() {
	log.Println("running in environment:", Environment)

	templates, static, err := Assets()
	if err != nil {
		log.Fatalf("load assets: %v", err)
	}

	mux := newMux(templates, static)
	log.Println("listening on :8080")
	srv := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	log.Fatal(srv.ListenAndServe())
}

// newMux wires up routes using the provided filesystems.
// Handler logic is identical regardless of dev/prod mode.
func newMux(templates fs.FS, static fs.FS) *http.ServeMux {
	tmpl, err := template.ParseFS(templates, "index.html")
	if err != nil {
		panic(fmt.Sprintf("parse template: %v", err))
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if err := tmpl.Execute(w, nil); err != nil {
			log.Printf("template execute error: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	})

	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))

	return mux
}
