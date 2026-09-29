// Package author holds the one shape every context uses to show who wrote,
// did or was mentioned in something. It lives outside the feature tree so
// that post, notification and feed can all name it without importing the
// user context, which owns the lookup behind it (service.AuthorDirectory).
package author
