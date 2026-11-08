// This program is intended to run as a sidecar of other, temporary containers.
// After startup, it begins to listen to incoming HTTP connections on port
// :8008. Requests to `/sync/:id` endpoint hang until canceled, and then
// trigger container `:id` to be terminated.
//
// If this program doesn't get any input for 10 seconds, it halts. It also
// terminates after an attempt to stop the requested container.
//
// Inspired by testcontainers/moby-ryuk project.
package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/orlangure/gnomock"
)

func main() {
	var (
		once      sync.Once
		connected = make(chan bool)
	)

	go func() {
		log.Println("waiting for input")

		select {
		case <-connected:
		case <-time.After(time.Second * 10):
			log.Fatalln("no incoming connections, stopping")
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	http.HandleFunc("/sync/", func(_ http.ResponseWriter, r *http.Request) {
		once.Do(func() { connected <- true })

		id := strings.TrimPrefix(r.URL.Path, "/sync/")
		log.Println("got request to kill", strconv.Quote(id))
		<-r.Context().Done()

		if err := gnomock.Stop(&gnomock.Container{
			ID: id,
		}); err != nil {
			log.Fatalln("can't stop container", strconv.Quote(id)+":", err.Error())
		}

		log.Println(strconv.Quote(id), "killed, exiting")
		os.Exit(0)
	})
	// Requests to /sync/ hang until the client disconnects, so there is no
	// read or write timeout; only the request header must arrive in time.
	srv := &http.Server{
		Addr:              ":8008",
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Fatalln(srv.ListenAndServe())
}
