package management

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

type ManagementService struct {
	// The management tunnel hostname
	Hostname string

	log    *zerolog.Logger
	router chi.Router
	logger LoggerListener
}

func New(managementHostname string, log *zerolog.Logger, logger LoggerListener) *ManagementService {
	s := &ManagementService{
		Hostname: managementHostname,
		log:      log,
		logger:   logger,
	}
	r := chi.NewRouter()
	r.Get("/ping", ping)
	r.Head("/ping", ping)
	s.router = r
	return s
}

func (m *ManagementService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.router.ServeHTTP(w, r)
}

// Management Ping handler
func ping(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
}
