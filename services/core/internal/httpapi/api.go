package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/minhhdtr/xsmb-discord-bot/internal/service"
	"github.com/minhhdtr/xsmb-discord-bot/internal/storage"
)

// Server is core's read API.
//
// It is deliberately unauthenticated. Nothing here is secret - the numbers are
// published on a public website - and the listener is bound inside the compose
// network, not to the host. Adding a token would be answering a question
// nobody asked while leaving the real one, which is that this must never be
// exposed publicly, to a comment.
type Server struct {
	svc   *service.Service
	store storage.Store
	gold  Gold // optional; nil disables the gold routes
	log   *slog.Logger
	mux   *http.ServeMux
}

// New builds the server and registers every route.
func New(svc *service.Service, store storage.Store, gold Gold, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{svc: svc, store: store, gold: gold, log: log, mux: http.NewServeMux()}

	// One table, in the same order as contracts/openapi.yaml. When the two
	// drift apart it should be visible in a single screen rather than spread
	// across a package.
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /v1/archive", s.archive)
	s.mux.HandleFunc("GET /v1/draws/latest", s.latestDraw)
	s.mux.HandleFunc("GET /v1/draws/{date}", s.draw)
	s.mux.HandleFunc("GET /v1/stats/day", s.dayReport)
	s.mux.HandleFunc("GET /v1/stats/gan", s.gan)
	s.mux.HandleFunc("GET /v1/stats/frequency", s.frequency)
	s.mux.HandleFunc("GET /v1/stats/number/{lo}", s.numberProfile)
	s.mux.HandleFunc("GET /v1/stats/special-month", s.specialMonth)
	s.mux.HandleFunc("POST /v1/spins", s.spin)
	s.mux.HandleFunc("GET /v1/gold", s.goldBoard)
	s.mux.HandleFunc("GET /v1/gold/{code}/history", s.goldHistory)
	s.mux.HandleFunc("GET /v1/gold/{code}/chart.png", s.goldChart)
	s.mux.HandleFunc("GET /v1/subscriptions", s.listSubscriptions)
	s.mux.HandleFunc("PUT /v1/subscriptions/{channel_id}", s.subscribe)
	s.mux.HandleFunc("DELETE /v1/subscriptions/{channel_id}", s.unsubscribe)
	s.mux.HandleFunc("POST /v1/announcements/{date}/{channel_id}", s.claimAnnouncement)
	s.mux.HandleFunc("DELETE /v1/announcements/{date}/{channel_id}", s.releaseAnnouncement)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	s.mux.ServeHTTP(recorder, r)

	// Logged at debug: on a healthy day this is one line per command, which is
	// noise in the bot's log but the first thing wanted when a client starts
	// getting shapes it did not expect.
	s.log.Debug("request", "method", r.Method, "path", r.URL.Path,
		"status", recorder.status, "took", time.Since(started))
}

// statusRecorder remembers the status code, which net/http does not expose
// once written.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

// Listen serves until ctx is cancelled, then shuts down gracefully so an
// in-flight crawl is not cut off mid-response.
func (s *Server) Listen(addr string, stop <-chan struct{}) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: s,
		// A crawl of a missing day can take a few seconds, and AwaitComplete
		// is not reachable from here, so this only has to outlast one fetch.
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errs := make(chan error, 1)
	go func() {
		s.log.Info("core api listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errs <- err
			return
		}
		errs <- nil
	}()

	select {
	case err := <-errs:
		return err
	case <-stop:
		s.log.Info("core api stopping")
		return srv.Close()
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, healthBody{Status: "ok"})
}
