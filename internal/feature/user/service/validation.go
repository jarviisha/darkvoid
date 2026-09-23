package service

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jarviisha/darkvoid/internal/feature/user"
	"github.com/jarviisha/darkvoid/internal/feature/user/dto"
	"github.com/jarviisha/darkvoid/pkg/errors"
)

var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,30}$`)
	letterRegex   = regexp.MustCompile(`[a-zA-Z]`)
	numberRegex   = regexp.MustCompile(`[0-9]`)
)

const (
	minPasswordLength    = 8
	maxPasswordLength    = 72
	minDisplayNameLength = 1
	maxDisplayNameLength = 100
)

func validateCreateRequest(req *dto.CreateUserRequest) error {
	if err := validateUsername(req.Username); err != nil {
		return err
	}
	if err := validateEmail(req.Email); err != nil {
		return err
	}
	if err := validateDisplayName(req.DisplayName); err != nil {
		return err
	}
	return validatePassword(req.Password)
}

func validateUpdateRequest(req *dto.UpdateUserRequest) error {
	if req.Email != nil {
		if *req.Email == "" {
			return errors.NewValidationError("email", "cannot be empty")
		}
		return validateEmail(*req.Email)
	}
	return nil
}

func validateUsername(username string) error {
	if err := requireField("username", username); err != nil {
		return err
	}
	if !usernameRegex.MatchString(username) {
		return errors.NewValidationError("username", "must be 3-30 alphanumeric characters, underscore, or hyphen")
	}
	return nil
}

func validateEmail(email string) error {
	if err := requireField("email", email); err != nil {
		return err
	}
	if !emailRegex.MatchString(email) {
		return errors.NewValidationError("email", "invalid format")
	}
	return nil
}

func validateDisplayName(displayName string) error {
	if err := requireField("display_name", displayName); err != nil {
		return err
	}
	return requireLength("display_name", displayName, minDisplayNameLength, maxDisplayNameLength)
}

func validatePassword(password string) error {
	if err := requireField("password", password); err != nil {
		return err
	}
	if len(password) < minPasswordLength {
		return user.ErrWeakPassword.WithDetail("min_length", minPasswordLength)
	}
	if len(password) > maxPasswordLength {
		return errors.NewValidationError("password", "too long").WithDetail("max_length", maxPasswordLength)
	}
	if !letterRegex.MatchString(password) || !numberRegex.MatchString(password) {
		return user.ErrWeakPassword.WithDetail("requirement", "must contain letters and numbers")
	}
	return nil
}

func requireField(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.NewValidationError(field, "required")
	}
	return nil
}

func requireLength(field, value string, min, max int) error {
	if length := len(strings.TrimSpace(value)); length < min || length > max {
		return errors.NewValidationError(field, "must be between "+strconv.Itoa(min)+" and "+strconv.Itoa(max)+" characters")
	}
	return nil
}
