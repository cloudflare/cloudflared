package management

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type ManagementService struct {
	// The management tunnel hostname
	Hostname string

	router chi.Router
}

func New(managementHostname string) *ManagementService {
	r := chi.NewRouter()
	r.Get("/ping", ping)
	return &ManagementService{
		Hostname: managementHostname,
		router:   r,
	}
}

func (m *ManagementService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.router.ServeHTTP(w, r)
}

func ping(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(200)
}
