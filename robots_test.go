package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckRobotsWith503(t *testing.T) {
	errserver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(503)
	}))
	defer errserver.Close()

	robots := NewRobots()

	ok, err := robots.Check(errserver.URL, NoAuthFn, "test-agent")
	if err != nil {
		t.Error(err)
	}
	if ok {
		t.Errorf("expected not ok got %v", ok)
	}
}

func TestCheckRobotsWith429(t *testing.T) {
	errserver := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(429)
	}))
	defer errserver.Close()

	robots := NewRobots()

	ok, err := robots.Check(errserver.URL, NoAuthFn, "test-agent")
	if err != nil {
		t.Error(err)
	}
	if !ok {
		t.Errorf("expected ok got %v", ok)
	}
}
