package main

import (
	"context"
	"errors"
	"net/http"
)

func Serve(ctx context.Context, mux *http.ServeMux, port string) error {
	var server = http.Server{
		Addr:    port,
		Handler: cors(mux),
	}

	go func() {
		<-ctx.Done()
		server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func serveDir(dir string, strip string) http.Handler {
	return http.StripPrefix(strip, http.FileServer(http.Dir(dir)))
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func nocache(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		fn(w, r)
	}
}
