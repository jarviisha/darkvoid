// Package dto holds the request and response shapes of the settings admin API.
//
// Durations cross the wire as whole seconds, matching both the columns that store
// them and bot.ConfigResponse. Update requests are partial and every field is a
// pointer, so "set this to false" stays distinguishable from "leave it alone" —
// which matters more here than usual, because several of these settings are kill
// switches whose whole purpose is being set to false.
package dto
