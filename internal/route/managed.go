package route

import (
	"net/http"

	"github.com/devchux/freesync/internal/managed"
)

// Managed registers the self-hosted managed-drive API.
func Managed(server *managed.Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/{action}", server.HandleAuth)
	mux.HandleFunc("GET /drives", server.HandleDrives)
	mux.HandleFunc("/drives/{drive}/{action}", server.HandleDrive)
	return server.Protect(mux)
}
