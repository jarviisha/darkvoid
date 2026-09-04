// Package repository provides persistence adapters for post-related data,
// including transaction-serialized like toggles.
//
// Every repository here holds its generated queries as the db.Querier
// interface rather than the concrete *db.Queries. That is the seam the tests
// substitute: binding to the concrete type again would make row mapping,
// cursor plumbing and error translation reachable only against a live
// database, which is where this package's coverage used to sit. Raw reads that
// sqlc cannot generate go through db.DBTX for the same reason.
package repository
