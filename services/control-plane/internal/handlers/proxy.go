package handlers

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// AnalyticsProxy proxies requests to the analytics service.
func (h *Handler) AnalyticsProxy() http.HandlerFunc {
	targetURL, err := url.Parse(h.cfg.AnalyticsURL)
	if err != nil {
		panic(err)
	}

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	originalDirector := proxy.Director

	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = targetURL.Host
	}

	return func(w http.ResponseWriter, r *http.Request) {
		proxy.ServeHTTP(w, r)
	}
}

