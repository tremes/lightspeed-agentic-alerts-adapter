package main

import "net/http"

// healthChecker reports whether the system is ready to serve traffic.
type healthChecker interface {
	Healthy() bool
}

// healthzHandler returns a liveness probe handler that always responds 200 OK.
func healthzHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// readyzHandler returns a readiness probe handler that responds 200 OK when
// the checker reports healthy, and 503 Service Unavailable otherwise.
func readyzHandler(checker healthChecker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checker.Healthy() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
}
