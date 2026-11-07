// Package gnomockd is an HTTP wrapper around Gnomock
package gnomockd

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/orlangure/gnomock/internal/errors"
)

// Handler returns an HTTP handler ready to serve incoming connections.
func Handler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("POST /start/{name}", startHandler())
	router.HandleFunc("POST /stop", stopHandler())

	return router
}

func respondWithError(w http.ResponseWriter, err error) {
	w.WriteHeader(errors.ErrorCode(err))

	err = json.NewEncoder(w).Encode(err)
	if err != nil {
		log.Println("can't respond with error:", err)
	}
}
