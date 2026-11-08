package gnomockd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/internal/errors"
	"github.com/orlangure/gnomock/internal/registry"
)

func startHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		p := registry.Find(name)

		if p == nil {
			respondWithError(w, errors.NewPresetNotFoundError(name))
			return
		}

		sr := &startRequest{Preset: p}
		decoder := json.NewDecoder(r.Body)

		err := decoder.Decode(&sr)
		if err != nil {
			respondWithError(w, errors.NewInvalidStartRequestError(err))
			return
		}

		started := make(chan bool)
		logWriter, allLogs := setupLogWriter(started)

		c, err := gnomock.Start(
			p,
			gnomock.WithOptions(&sr.Options),
			gnomock.WithLogWriter(logWriter),
			gnomock.WithContext(r.Context()),
		)

		close(started)

		if err != nil {
			err = fmt.Errorf("%s: %w", strings.Join(<-allLogs, ";"), err)
			respondWithError(w, errors.NewStartFailedError(err, c))

			return
		}

		err = json.NewEncoder(w).Encode(c)
		if err != nil {
			respondWithError(w, errors.NewStartFailedError(err, c))
			return
		}
	}
}

type startRequest struct {
	Options gnomock.Options `json:"options"`
	Preset  gnomock.Preset  `json:"preset"`
}
