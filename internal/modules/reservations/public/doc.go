// Package public exposes the reservations module's interfaces (ports) and plain DTOs
// for consumption by OTHER modules. This is the only importable surface of
// the module from outside it.
//
// Allowed imports: stdlib, own DTOs only.
// Forbidden imports: own domain (DTOs are standalone, never domain types).
package public
