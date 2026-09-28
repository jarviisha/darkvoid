// Package entity holds the domain types for runtime-tunable application
// settings.
//
// FeedSettings is the whole of it today: the feed knobs that used to be FEED_*
// environment variables, plus the ranking weights that were literals in the feed
// package: the three local ones and the recommendation weight. The type carries
// its own bounds and validation so that a bad value is a 400 naming the field
// rather than a CHECK violation surfacing as a 500.
package entity
