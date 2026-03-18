package handler

import (
	"embed"
	"net/http"
)

//go:embed static/admin.html
var adminHTML embed.FS

// HandleAdminUI serves the admin dashboard.
func HandleAdminUI(w http.ResponseWriter, r *http.Request) {
	data, _ := adminHTML.ReadFile("static/admin.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}
