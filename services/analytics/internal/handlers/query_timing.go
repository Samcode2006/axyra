package handlers

import (
	"net/http"
	"strconv"
	"time"
)

// measureDBQuery times the database call through result scanning. Keep only
// DB execution and result scanning in query: authorization, limiter waits,
// response encoding and latency-observation writes are outside this interval.
// Database connection-pool waits and the Go-to-DB round trip are included.
func measureDBQuery(w http.ResponseWriter, query func() error) error {
	start := time.Now()
	err := query()
	elapsed := time.Since(start)
	if err == nil {
		w.Header().Set("X-DB-Query-Ms", strconv.FormatFloat(float64(elapsed)/float64(time.Millisecond), 'f', 3, 64))
	}
	return err
}
