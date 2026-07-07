package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

// Renderer executes html/template pages parsed from an embedded filesystem.
// Each page is its own clone of the base layout so pages don't clobber each
// other's "content" block definition.
type Renderer struct {
	pages map[string]*template.Template
}

// NewRenderer parses templates/base.html plus every other templates/*.html
// file, cloning the base layout per page. A parse failure returns an error
// (never panics).
func NewRenderer(fsys embed.FS) (*Renderer, error) {
	base, err := template.ParseFS(fsys, "templates/base.html")
	if err != nil {
		return nil, fmt.Errorf("parse base template: %w", err)
	}

	entries, err := fs.Glob(fsys, "templates/*.html")
	if err != nil {
		return nil, err
	}

	pages := map[string]*template.Template{}
	for _, entry := range entries {
		name := strings.TrimSuffix(filepath.Base(entry), ".html")
		if name == "base" {
			continue
		}
		clone, err := base.Clone()
		if err != nil {
			return nil, fmt.Errorf("clone base template for page %s: %w", name, err)
		}
		clone, err = clone.ParseFS(fsys, entry)
		if err != nil {
			return nil, fmt.Errorf("parse page template %s: %w", name, err)
		}
		pages[name] = clone
	}

	return &Renderer{pages: pages}, nil
}

// Page renders the named page: the full base-layout shell for a plain
// request, or just the "content" fragment when the request carries
// HX-Request: true (an htmx swap).
func (r *Renderer) Page(w http.ResponseWriter, req *http.Request, page string, data any) error {
	tmpl, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("unknown page %q", page)
	}
	if req.Header.Get("HX-Request") == "true" {
		return tmpl.ExecuteTemplate(w, "content", data)
	}
	return tmpl.ExecuteTemplate(w, "base", data)
}
