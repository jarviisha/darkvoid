package service

import (
	"strings"
	"testing"
)

// The length rule counts characters at the bottom, where it is a strength
// rule, and bytes at the top, where it is bcrypt's limit. Letters are any
// script's, so a password written in Cyrillic or Vietnamese is not refused
// as having none.
func TestValidatePassword_Boundaries(t *testing.T) {
	cases := map[string]struct {
		password string
		code     string // empty means accepted
	}{
		"8 characters":              {"Abcdefg1", ""},
		"7 characters":              {"Abcdef1", "WEAK_PASSWORD"},
		"72 bytes":                  {strings.Repeat("a1", 36), ""},
		"73 bytes":                  {strings.Repeat("a1", 36) + "a", "VALIDATION_ERROR"},
		"cyrillic letters":          {"пароль12", ""},
		"vietnamese letters":        {"mậtkhẩu1", ""},
		"6 characters in 10 bytes":  {"абвг1a", "WEAK_PASSWORD"},
		"37 characters in 72 bytes": {strings.Repeat("я", 35) + "12", ""},
		"38 characters in 74 bytes": {strings.Repeat("я", 36) + "12", "VALIDATION_ERROR"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := validatePassword("password", tc.password)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("expected %q accepted, got %v", tc.password, err)
				}
				return
			}
			assertServiceErrorCode(t, err, tc.code)
		})
	}
}
