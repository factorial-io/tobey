package main

import (
	"log"
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"
)

var (
	router         = mux.NewRouter()
	healtcheckPort = ":10241"
)

func startHealthcheck() {
	router.HandleFunc("/healthz", healtcheck).Methods("GET", "HEAD").Name("Healthcheck")
	slog.Info("Healthcheck handler is listening.", "port", healtcheckPort)
	log.Fatal(http.ListenAndServe(healtcheckPort, router))
}

func healtcheck(w http.ResponseWriter, req *http.Request) {
	//Todo add logic
	w.Write([]byte("OK"))
}
