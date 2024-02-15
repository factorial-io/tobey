package main

import (
	"net/http"

	logger "tobey/logger"

	"github.com/gorilla/mux"
)

var (
	router         = mux.NewRouter()
	healtcheckPort = ":10241"
)

func startHealthcheck() {
	log := logger.GetBaseLogger()
	router.HandleFunc("/healthz", healtcheck).Methods("GET", "HEAD").Name("Healthcheck")
	log.Info("Healthcheck handler is listening on ", healtcheckPort)
	log.Fatal(http.ListenAndServe(healtcheckPort, router))
}

func healtcheck(w http.ResponseWriter, req *http.Request) {
	//Todo add logic
	w.Write([]byte("OK"))
}
