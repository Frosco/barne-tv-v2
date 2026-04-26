package main

import (
	"html/template"
	"net/http"
	"strings"
)

type GridHandler struct {
	Cache    *VideoCache
	Template *template.Template
	GridSize int
}

type templateData struct {
	Videos []Video
}

func (h *GridHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	shuffle := r.URL.Query().Get("shuffle") != ""

	var videos []Video

	if !shuffle {
		if cookie, err := r.Cookie("grid"); err == nil {
			ids := strings.Split(cookie.Value, ",")
			videos = h.Cache.GetByIDs(ids)
		}
	}

	if videos == nil {
		perSourceCap := h.GridSize / 5
		if perSourceCap < 1 {
			perSourceCap = 1
		}
		videos = h.Cache.RandomCapped(h.GridSize, perSourceCap)
	}

	// Set cookie with current selection
	if len(videos) > 0 {
		ids := make([]string, len(videos))
		for i, v := range videos {
			ids[i] = v.ID
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "grid",
			Value:    strings.Join(ids, ","),
			Path:     "/",
			SameSite: http.SameSiteLaxMode,
		})
	}

	data := templateData{Videos: videos}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.Template.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
