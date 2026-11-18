package main

import (
	"context"
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
		srv           = http.NewServeMux()
		bgctx, stop   = signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		stopConsumers = LoadConsumers(bgctx)
	)
	defer stop()
	defer stopConsumers()

	// enque and create container
	srv.HandleFunc("GET /connect", nocache(func(w http.ResponseWriter, r *http.Request) {
		var (
			ctx = r.Context()
			req = NewPlayReqest(ctx)
		)
		if req == nil {
			http.Error(w, "Cannot connect to playground: too many requests", http.StatusServiceUnavailable)
			return
		}

		select {
		case play := <-req.created:
			if play != nil {
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(req.id))
				return
			}
			http.Error(w, req.err.Error(), http.StatusInternalServerError)
		case <-ctx.Done():
			http.Error(w, "Connection interrupted", http.StatusBadRequest)
		case <-bgctx.Done():
			http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
		}
		req.Abort()
	}))

	// edit page
	srv.HandleFunc("PUT /{ID}/{FILE}", func(w http.ResponseWriter, r *http.Request) {
		play, err := ConnectPlayRequest(r.PathValue("ID"))
		if err != nil {
			http.Error(w, "Cannot connect to playground: "+err.Error(), http.StatusGone)
			return
		}

		content, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Cannot read body: "+err.Error(), http.StatusUnsupportedMediaType)
			return
		}
		defer r.Body.Close()

		file := r.PathValue("FILE")
		if !filenamergx.MatchString(file) {
			http.Error(w, "Cannot edit file '"+file+"' outside of boundaries", 403)
			return
		}

		out, err := play.Edit(file, content)
		if err != nil {
			http.Error(w, string(out)+"\n"+err.Error(), http.StatusBadRequest)
			return
		}
		w.Write(out)
	})

	srv.HandleFunc("DELETE /{ID}/{FILE}", func(w http.ResponseWriter, r *http.Request) {
		play, err := ConnectPlayRequest(r.PathValue("ID"))
		if err != nil {
			http.Error(w, "Cannot connect to playground: "+err.Error(), http.StatusGone)
			return
		}

		file := r.PathValue("FILE")
		if !filenamergx.MatchString(file) {
			http.Error(w, "Cannot edit file '"+file+"' outside of boundaries", 403)
			return
		}

		out, err := play.Delete(file)
		if err != nil {
			http.Error(w, string(out)+"\n"+err.Error(), http.StatusBadRequest)
			return
		}
		w.Write(out)
	})

	srv.HandleFunc("DELETE /{ID}", func(w http.ResponseWriter, r *http.Request) {
		play, err := ConnectPlayRequest(r.PathValue("ID"))
		if err != nil {
			http.Error(w, "Cannot connect to playground: "+err.Error(), http.StatusGone)
		} else if err = play.Close(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	srv.HandleFunc("GET /raw/{ID}/", func(w http.ResponseWriter, r *http.Request) {
		var reqID = r.PathValue("ID")
		serveDir(
			filepath.Join(os.TempDir(), reqID+"_playground"),
			"/raw/"+reqID+"/",
		).ServeHTTP(w, r)
	})

	srv.HandleFunc("GET /preview/{ID}/", func(w http.ResponseWriter, r *http.Request) {
		var reqID = r.PathValue("ID")
		serveDir(
			filepath.Join(os.TempDir(), reqID+"_playground", "build"),
			"/preview/"+reqID+"/",
		).ServeHTTP(w, r)
	})

	srv.Handle("GET /", serveDir("./static", "/"))

	if err := Serve(bgctx, srv, port); err != nil {
		fmt.Println("server error:", err)
	}
}
