package service

import (
	"strings"
	"testing"

	"github.com/jarviisha/darkvoid/pkg/errors"
)

// rejectedPasswords are new passwords every flow that sets one must refuse,
// with the error code each draws. Past 72 bytes bcrypt refuses to hash, so
// that case has to be caught before hashing or it surfaces as a 500.
var rejectedPasswords = map[string]struct{ password, code string }{
	"too short":         {"Short12", "WEAK_PASSWORD"},
	"no digit":          {"OnlyLetters", "WEAK_PASSWORD"},
	"no letter":         {"1234567890", "WEAK_PASSWORD"},
	"over bcrypt limit": {strings.Repeat("a1", 37), "VALIDATION_ERROR"},
}

func assertErrorField(t *testing.T, err error, field string) {
	t.Helper()
	if appErr := errors.GetAppError(err); appErr == nil || appErr.Details["field"] != field {
		t.Errorf("expected error naming field %q, got %v", field, err)
	}
}
