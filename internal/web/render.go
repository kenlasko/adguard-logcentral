package web

import (
	"bytes"
	"net/http"
)

// renderPage renders a full page (layout + content). Rendering goes through a
// buffer so a template error yields a clean 500 rather than a half-written body.
func (s *Server) renderPage(w http.ResponseWriter, page string, data any) {
	set, ok := s.templates.pages[page]
	if !ok {
		s.logger.Error("unknown page template", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, "layout.html", data); err != nil {
		s.logger.Error("render page failed", "page", page, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// renderFragment renders a single named partial for an htmx swap.
func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := s.templates.fragments.ExecuteTemplate(&buf, name, data); err != nil {
		s.logger.Error("render fragment failed", "fragment", name, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
