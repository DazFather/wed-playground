package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

const (
	MAX_WAITTIME      = 2 * time.Minute
	MAX_INACTIVE_TIME = 15 * time.Minute
	MAX_PLAYTIME      = 2 * time.Hour
	LIVENESS_PROBE    = 3 * time.Second
)

const (
	MAX_CONCURRENT_PLAYS = 2
	WAITLIST_SIZE        = 2
)

func main() {
	var (
		filenamergx   = regexp.MustCompile(`^[\w\-]+\.(tmpl|wed\.html)$`)
		mux           = http.NewServeMux()
		bgctx, stop   = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		stopConsumers = LoadConsumers(bgctx)
	)
	defer stop()
	defer stopConsumers()

	// enque and create container
	mux.HandleFunc("GET /connect", func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx = r.Context()
			req = NewPlayReqest(ctx)
		)
		if req == nil {
			http.Error(w, "Cannot connect to playground: too many requests", 503)
			return
		}

		select {
		case play := <-req.created:
			if play != nil {
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(req.id))
				return
			}
			http.Error(w, req.err.Error(), 500)
		case <-ctx.Done():
			http.Error(w, "Connection interrupted", 400)
		case <-bgctx.Done():
			http.Error(w, "Server is shutting down", 503)
		}
		req.Abort()
	})

	// edit page
	mux.HandleFunc("PUT /{ID}/{FILE}", func(w http.ResponseWriter, r *http.Request) {
		play, err := ConnectPlayground(r.PathValue("ID"))
		if err != nil {
			http.Error(w, "Cannot connect to playground: "+err.Error(), 500)
			return
		}

		content, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Cannot read body: "+err.Error(), 400)
			return
		}
		defer r.Body.Close()

		file := r.PathValue("FILE")
		if !filenamergx.MatchString(file) {
			http.Error(w, "Cannot edit file '"+file+"' outside of boundaries", 400)
			return
		}

		out, err := play.Edit(file, content)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Write(out)
	})

	mux.HandleFunc("DELETE /{ID}", func(w http.ResponseWriter, r *http.Request) {
		play, err := ConnectPlayground(r.PathValue("ID"))
		if err != nil {
			http.Error(w, "Cannot connect to playground: "+err.Error(), 500)
			return
		}
		if err = play.Close(); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})

	mux.HandleFunc("GET /raw/{ID}/", func(w http.ResponseWriter, r *http.Request) {
		var (
			reqID = r.PathValue("ID")
			dir   = filepath.Join(os.TempDir(), reqID+"_playground")
		)
		next := http.StripPrefix(
			"/raw/"+reqID+"/",
			http.FileServer(http.Dir(dir)),
		)
		next.ServeHTTP(w, r)
	})

	mux.HandleFunc("GET /preview/{ID}/", func(w http.ResponseWriter, r *http.Request) {
		var (
			reqID = r.PathValue("ID")
			dir   = filepath.Join(os.TempDir(), reqID+"_playground", "build")
		)
		next := http.StripPrefix(
			"/preview/"+reqID+"/",
			http.FileServer(http.Dir(dir)),
		)
		next.ServeHTTP(w, r)
	})

	mux.Handle("GET /", http.StripPrefix(
		"/", http.FileServer(http.Dir("./static")),
	))

	srv := &http.Server{
		Addr:    ":8080",
		Handler: cors(mux),
	}

	go func() {
		<-bgctx.Done()
		srv.Shutdown(bgctx)
		fmt.Println("Gracefully shutting down server")
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Println("server error:", err)
	}
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
