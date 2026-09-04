// Package repository provides persistence adapters for post-related data,
// including transaction-serialized like toggles.
//
// Every repository here holds its generated queries as the db.Querier
// interface rather than the concrete *db.Queries. That is the seam the tests
// substitute: binding to the concrete type again would make row mapping,
// cursor plumbing and error translation reachable only against a live
// database, which is where this package's coverage used to sit. Raw reads that
// sqlc cannot generate go through db.DBTX for the same reason.
//
// The seam stops at the single-statement path. Toggle, UpsertAndLink and
// ReplaceForPost open their own transaction through a concrete *pgxpool.Pool,
// and WithTx builds its queries from the transaction rather than from the
// field, so neither is reachable from a test double — those paths are covered
// against a real Postgres or not at all. That is deliberate: what Toggle
// actually does is serialize two concurrent callers on an advisory lock, and a
// fake transaction would assert the shape of the code while proving nothing
// about the behaviour the lock exists for.
package repository
