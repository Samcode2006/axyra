package handlers

import (
	"net/http"
	"strconv"
	"time"
)

// window is a half-open range of whole UTC days: [Start, End).
//
// The expansion endpoints read day-bucketed aggregates, so every one of them
// rounds its range outward to whole UTC days. Using one rule everywhere keeps
// panels on the same page consistent with each other.
type window struct {
	Start, End time.Time
}

func startOfDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// parseWindow reads ?from= and ?to= (RFC 3339). Missing values default to the
// last 7 days. It writes a 400 and returns ok=false for an unparseable or
// reversed range. The result covers the whole UTC days containing from and to.
func parseWindow(w http.ResponseWriter, r *http.Request) (window, bool) {
	now := time.Now().UTC()
	from, to := now.Add(-7*24*time.Hour), now

	if s := r.URL.Query().Get("from"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "from must be an RFC 3339 timestamp")
			return window{}, false
		}
		from = t
	}
	if s := r.URL.Query().Get("to"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			respondError(w, http.StatusBadRequest, "to must be an RFC 3339 timestamp")
			return window{}, false
		}
		to = t
	}
	if from.After(to) {
		respondError(w, http.StatusBadRequest, "from must not be after to")
		return window{}, false
	}
	return window{Start: startOfDay(from), End: startOfDay(to).AddDate(0, 0, 1)}, true
}

// intParam reads a bounded integer query parameter, falling back to def.
func intParam(r *http.Request, key string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// pct returns part/whole as a percentage rounded to one decimal, or 0 when whole is 0.
func pct(part, whole float64) float64 {
	if whole == 0 {
		return 0
	}
	return round1(part * 100 / whole)
}

func round1(f float64) float64 { return float64(int64(f*10+0.5*sign(f))) / 10 }

func round2(f float64) float64 { return float64(int64(f*100+0.5*sign(f))) / 100 }

func sign(f float64) float64 {
	if f < 0 {
		return -1
	}
	return 1
}

// ratio returns num/den rounded to 2 decimals, or 0 when den is 0.
func ratio(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return round2(num / den)
}

// orUnknown labels an empty dimension value.
func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// explicitTenant returns the tenant named by the request (query or header),
// resolved to its canonical id, or "" when none was named. Unlike
// resolveRequestTenant it never falls back to a default tenant, so platform-wide
// views stay platform-wide when no tenant is given.
func (h *Handler) explicitTenant(r *http.Request) string {
	t := r.URL.Query().Get("tenant_id")
	if t == "" {
		t = r.URL.Query().Get("external_tenant_id")
	}
	if t == "" {
		t = r.Header.Get("X-Tenant-ID")
	}
	if t == "" {
		return ""
	}
	return h.resolveRequestTenant(r)
}
