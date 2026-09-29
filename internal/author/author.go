package author

import "github.com/google/uuid"

// Author is the minimal public identity of a user.
type Author struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
	AvatarKey   *string
}
