package handlers

import (
	"errors"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestDBQueryTimingExcludesOtherWork(t *testing.T) {
	w := httptest.NewRecorder()
	start := time.Now()
	time.Sleep(15 * time.Millisecond) // authorization / limiter wait
	var queryElapsed time.Duration
	err := measureDBQuery(w, func() error {
		queryStart := time.Now()
		time.Sleep(5 * time.Millisecond) // DB execution and Scan
		queryElapsed = time.Since(queryStart)
		return nil
	})
	time.Sleep(15 * time.Millisecond) // encoding / other queries
	ms, parseErr := strconv.ParseFloat(w.Header().Get("X-DB-Query-Ms"), 64)
	if err != nil || parseErr != nil {
		t.Fatalf("query=%v header=%v", err, parseErr)
	}
	if ms+0.001 < float64(queryElapsed)/float64(time.Millisecond) {
		t.Fatal("DB work omitted from timing")
	}
	if float64(time.Since(start))/float64(time.Millisecond)-ms < 30 {
		t.Fatal("non-query work included in timing")
	}
}

func TestDBQueryTimingDoesNotReportFailedQuery(t *testing.T) {
	w := httptest.NewRecorder()
	want := errors.New("database unavailable")
	if got := measureDBQuery(w, func() error { return want }); got != want {
		t.Fatalf("got %v, want original database error", got)
	}
	if w.Header().Get("X-DB-Query-Ms") != "" {
		t.Fatal("failed query advertised as a successful measurement")
	}
}
