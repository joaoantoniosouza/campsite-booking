package web

import "net/http"

// Home renders the base page at GET /.
func Home(r *Renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		if err := r.Page(w, req, "home", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// NotFound renders the 404 page.
func NotFound(r *Renderer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		if err := r.Page(w, req, "not_found", nil); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}
