package handler

import (
	"embed"
	"net/http"
)

//go:embed static/index.html
var indexHTML embed.FS

//go:embed static/app.html
var appHTML embed.FS

// HandleIndex serves the landing page.
func HandleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := indexHTML.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

// HandleApp serves the miner dashboard SPA.
func HandleApp(w http.ResponseWriter, r *http.Request) {
	data, err := appHTML.ReadFile("static/app.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
