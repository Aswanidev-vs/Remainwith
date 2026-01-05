package handler

import (
	"log"
	"net/http"
	"time"
)

func Logger(logs http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		logs.ServeHTTP(w, r)

		log.Printf(
			"%s %s %s %v",
			r.RemoteAddr,
			r.Method,
			r.URL.Path,
			time.Since(start),
		)
	})
}
