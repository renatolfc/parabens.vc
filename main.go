package main

import (
	"embed"
	"log/slog"
	"net/http"
	"os"
	"time"
)

const (
	maxTrackBodyBytes     = 16 * 1024
	maxPathLen            = 512
	maxShortlinkBodyBytes = 8 * 1024
	shortCodeLen          = 7
	shortlinkRateLimit    = 20
	shortlinkRateWindow   = time.Minute
	trackRateLimit        = 120
	trackRateWindow       = time.Minute
	ogImageWidth          = 600
	ogImageHeight         = 315
	ogImageTextLimit      = 39
	ogRenderTimeout       = 5 * time.Second
	siteDomain            = "parabens.vc"
)

//go:embed public/index.html public/privacy.html public/styles.css public/app.js public/favicon.svg public/og-image.svg public/og-image.png public/og-template.svg public/blocked-words.txt
var embeddedFiles embed.FS

type TrackEvent struct {
	Path           string      `json:"path,omitempty"`
	UserAgent      string      `json:"user_agent,omitempty"`
	Timestamp      string      `json:"timestamp,omitempty"`
	Event          string      `json:"event,omitempty"`
	Query          string      `json:"query,omitempty"`
	Referrer       string      `json:"referrer,omitempty"`
	AcceptLanguage string      `json:"accept_language,omitempty"`
	Timezone       string      `json:"timezone,omitempty"`
	Screen         interface{} `json:"screen,omitempty"`
	Viewport       interface{} `json:"viewport,omitempty"`
}

type ShortLinkRequest struct {
	Path string `json:"path"`
}

type ShortLinkResponse struct {
	Code        string `json:"code"`
	ShortURL    string `json:"short_url"`
	Path        string `json:"path"`
	Destination string `json:"destination"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/track", handleTrack)
	mux.HandleFunc("/s", handleShortlinkCreate)
	mux.HandleFunc("/s/", handleShortlinkRedirect)
	mux.HandleFunc("/og-image.png", handleOgImage)
	mux.HandleFunc("/", handlePage)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           withRequestLogging(withSecurityHeaders(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	slog.Info("server starting", "addr", "0.0.0.0:"+port)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server error", "error", err)
	}
}
