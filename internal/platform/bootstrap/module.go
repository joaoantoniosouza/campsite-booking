package bootstrap

import "github.com/go-chi/chi/v5"

// Module is the only surface bootstrap needs to mount a module's HTTP
// routes. Each module's adapter/http provides an implementation; bootstrap
// wires the module's app-layer dependencies into it BEFORE calling Mount,
// keeping bootstrap free of module internals.
type Module interface {
	Name() string       // e.g. "reservations" — for logging/route grouping
	Mount(r chi.Router) // registers the module's routes on the shared router
}
