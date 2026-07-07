// Package repository implements the checkin module's domain repository
// interfaces using infrastructure libraries (pgx/sqlc).
//
// Allowed imports: own app, own domain, infra libs (pgx, sqlc-generated code).
// Forbidden imports: other modules' internals (domain, app, adapter).
package repository
