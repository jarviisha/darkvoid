package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
)

// mockQuerier implements db.Querier for unit tests.
// Set only the function fields exercised by the test under execution.
// Unset fields return zero values and nil errors.
type mockQuerier struct {
	adminCountUsers                func(context.Context, db.AdminCountUsersParams) (int64, error)
	adminListUsers                 func(context.Context, db.AdminListUsersParams) ([]db.UsrUser, error)
	adminSetUserActive             func(context.Context, db.AdminSetUserActiveParams) error
	assignRoleToUser               func(context.Context, db.AssignRoleToUserParams) error
	checkUserHasRole               func(context.Context, db.CheckUserHasRoleParams) (bool, error)
	countFollowers                 func(context.Context, uuid.UUID) (int64, error)
	countFollowing                 func(context.Context, uuid.UUID) (int64, error)
	countSearchUsers               func(context.Context, *string) (int64, error)
	createEmailToken               func(context.Context, db.CreateEmailTokenParams) (db.UsrEmailToken, error)
	createRefreshToken             func(context.Context, db.CreateRefreshTokenParams) (db.UsrRefreshToken, error)
	createRole                     func(context.Context, db.CreateRoleParams) (db.UsrRole, error)
	createUser                     func(context.Context, db.CreateUserParams) (db.UsrUser, error)
	deactivateUser                 func(context.Context, db.DeactivateUserParams) error
	deleteEmailTokensByUserAndType func(context.Context, db.DeleteEmailTokensByUserAndTypeParams) error
	deleteExpiredEmailTokens       func(context.Context) error
	deleteExpiredRefreshTokens     func(context.Context) error
	existsEmail                    func(context.Context, string) (bool, error)
	existsEmailExcludingUser       func(context.Context, db.ExistsEmailExcludingUserParams) (bool, error)
	existsUsername                 func(context.Context, string) (bool, error)
	follow                         func(context.Context, db.FollowParams) error
	getActiveRefreshTokensByUserID func(context.Context, uuid.UUID) ([]db.UsrRefreshToken, error)
	getEmailTokenByToken           func(context.Context, string) (db.UsrEmailToken, error)
	getFollowers                   func(context.Context, db.GetFollowersParams) ([]db.UsrFollow, error)
	getFollowing                   func(context.Context, db.GetFollowingParams) ([]db.UsrFollow, error)
	getRefreshTokenByToken         func(context.Context, string) (db.UsrRefreshToken, error)
	getRoleByID                    func(context.Context, uuid.UUID) (db.UsrRole, error)
	getRoleByName                  func(context.Context, string) (db.UsrRole, error)
	getUserByEmail                 func(context.Context, string) (db.UsrUser, error)
	getUserByID                    func(context.Context, uuid.UUID) (db.UsrUser, error)
	getUserByIDAny                 func(context.Context, uuid.UUID) (db.UsrUser, error)
	getUserByUsername              func(context.Context, string) (db.UsrUser, error)
	getUserRoles                   func(context.Context, uuid.UUID) ([]db.UsrRole, error)
	getUsersByIDs                  func(context.Context, []uuid.UUID) ([]db.UsrUser, error)
	getUsersByIDsAny               func(context.Context, []uuid.UUID) ([]db.UsrUser, error)
	getUsersByUsernames            func(context.Context, []string) ([]db.UsrUser, error)
	isFollowing                    func(context.Context, db.IsFollowingParams) (bool, error)
	listAllActiveUserIDs           func(context.Context) ([]uuid.UUID, error)
	listRoles                      func(context.Context) ([]db.UsrRole, error)
	markEmailTokenUsed             func(context.Context, uuid.UUID) error
	removeRoleFromUser             func(context.Context, db.RemoveRoleFromUserParams) error
	revokeAllUserRefreshTokens     func(context.Context, uuid.UUID) error
	revokeRefreshToken             func(context.Context, string) error
	searchUsers                    func(context.Context, db.SearchUsersParams) ([]db.UsrUser, error)
	searchUsersByQuery             func(context.Context, db.SearchUsersByQueryParams) ([]db.UsrUser, error)
	unfollow                       func(context.Context, db.UnfollowParams) error
	updateUser                     func(context.Context, db.UpdateUserParams) (db.UsrUser, error)
	updateUserPassword             func(context.Context, db.UpdateUserPasswordParams) error
	updateUserProfile              func(context.Context, db.UpdateUserProfileParams) (db.UsrUser, error)
}

func (m *mockQuerier) AdminCountUsers(ctx context.Context, arg db.AdminCountUsersParams) (int64, error) {
	if m.adminCountUsers != nil {
		return m.adminCountUsers(ctx, arg)
	}
	return 0, nil
}

func (m *mockQuerier) AdminListUsers(ctx context.Context, arg db.AdminListUsersParams) ([]db.UsrUser, error) {
	if m.adminListUsers != nil {
		return m.adminListUsers(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) AdminSetUserActive(ctx context.Context, arg db.AdminSetUserActiveParams) error {
	if m.adminSetUserActive != nil {
		return m.adminSetUserActive(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) AssignRoleToUser(ctx context.Context, arg db.AssignRoleToUserParams) error {
	if m.assignRoleToUser != nil {
		return m.assignRoleToUser(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) CheckUserHasRole(ctx context.Context, arg db.CheckUserHasRoleParams) (bool, error) {
	if m.checkUserHasRole != nil {
		return m.checkUserHasRole(ctx, arg)
	}
	return false, nil
}

func (m *mockQuerier) CountFollowers(ctx context.Context, followeeID uuid.UUID) (int64, error) {
	if m.countFollowers != nil {
		return m.countFollowers(ctx, followeeID)
	}
	return 0, nil
}

func (m *mockQuerier) CountFollowing(ctx context.Context, followerID uuid.UUID) (int64, error) {
	if m.countFollowing != nil {
		return m.countFollowing(ctx, followerID)
	}
	return 0, nil
}

func (m *mockQuerier) CountSearchUsers(ctx context.Context, query *string) (int64, error) {
	if m.countSearchUsers != nil {
		return m.countSearchUsers(ctx, query)
	}
	return 0, nil
}

func (m *mockQuerier) CreateEmailToken(ctx context.Context, arg db.CreateEmailTokenParams) (db.UsrEmailToken, error) {
	if m.createEmailToken != nil {
		return m.createEmailToken(ctx, arg)
	}
	return db.UsrEmailToken{}, nil
}

func (m *mockQuerier) CreateRefreshToken(ctx context.Context, arg db.CreateRefreshTokenParams) (db.UsrRefreshToken, error) {
	if m.createRefreshToken != nil {
		return m.createRefreshToken(ctx, arg)
	}
	return db.UsrRefreshToken{}, nil
}

func (m *mockQuerier) CreateRole(ctx context.Context, arg db.CreateRoleParams) (db.UsrRole, error) {
	if m.createRole != nil {
		return m.createRole(ctx, arg)
	}
	return db.UsrRole{}, nil
}

func (m *mockQuerier) CreateUser(ctx context.Context, arg db.CreateUserParams) (db.UsrUser, error) {
	if m.createUser != nil {
		return m.createUser(ctx, arg)
	}
	return db.UsrUser{}, nil
}

func (m *mockQuerier) DeactivateUser(ctx context.Context, arg db.DeactivateUserParams) error {
	if m.deactivateUser != nil {
		return m.deactivateUser(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) DeleteEmailTokensByUserAndType(ctx context.Context, arg db.DeleteEmailTokensByUserAndTypeParams) error {
	if m.deleteEmailTokensByUserAndType != nil {
		return m.deleteEmailTokensByUserAndType(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) DeleteExpiredEmailTokens(ctx context.Context) error {
	if m.deleteExpiredEmailTokens != nil {
		return m.deleteExpiredEmailTokens(ctx)
	}
	return nil
}

func (m *mockQuerier) DeleteExpiredRefreshTokens(ctx context.Context) error {
	if m.deleteExpiredRefreshTokens != nil {
		return m.deleteExpiredRefreshTokens(ctx)
	}
	return nil
}

func (m *mockQuerier) ExistsEmail(ctx context.Context, email string) (bool, error) {
	if m.existsEmail != nil {
		return m.existsEmail(ctx, email)
	}
	return false, nil
}

func (m *mockQuerier) ExistsEmailExcludingUser(ctx context.Context, arg db.ExistsEmailExcludingUserParams) (bool, error) {
	if m.existsEmailExcludingUser != nil {
		return m.existsEmailExcludingUser(ctx, arg)
	}
	return false, nil
}

func (m *mockQuerier) ExistsUsername(ctx context.Context, username string) (bool, error) {
	if m.existsUsername != nil {
		return m.existsUsername(ctx, username)
	}
	return false, nil
}

func (m *mockQuerier) Follow(ctx context.Context, arg db.FollowParams) error {
	if m.follow != nil {
		return m.follow(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) GetActiveRefreshTokensByUserID(ctx context.Context, userID uuid.UUID) ([]db.UsrRefreshToken, error) {
	if m.getActiveRefreshTokensByUserID != nil {
		return m.getActiveRefreshTokensByUserID(ctx, userID)
	}
	return nil, nil
}

func (m *mockQuerier) GetEmailTokenByToken(ctx context.Context, token string) (db.UsrEmailToken, error) {
	if m.getEmailTokenByToken != nil {
		return m.getEmailTokenByToken(ctx, token)
	}
	return db.UsrEmailToken{}, nil
}

func (m *mockQuerier) GetFollowers(ctx context.Context, arg db.GetFollowersParams) ([]db.UsrFollow, error) {
	if m.getFollowers != nil {
		return m.getFollowers(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) GetFollowing(ctx context.Context, arg db.GetFollowingParams) ([]db.UsrFollow, error) {
	if m.getFollowing != nil {
		return m.getFollowing(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) GetRefreshTokenByToken(ctx context.Context, token string) (db.UsrRefreshToken, error) {
	if m.getRefreshTokenByToken != nil {
		return m.getRefreshTokenByToken(ctx, token)
	}
	return db.UsrRefreshToken{}, nil
}

func (m *mockQuerier) GetRoleByID(ctx context.Context, id uuid.UUID) (db.UsrRole, error) {
	if m.getRoleByID != nil {
		return m.getRoleByID(ctx, id)
	}
	return db.UsrRole{}, nil
}

func (m *mockQuerier) GetRoleByName(ctx context.Context, name string) (db.UsrRole, error) {
	if m.getRoleByName != nil {
		return m.getRoleByName(ctx, name)
	}
	return db.UsrRole{}, nil
}

func (m *mockQuerier) GetUserByEmail(ctx context.Context, email string) (db.UsrUser, error) {
	if m.getUserByEmail != nil {
		return m.getUserByEmail(ctx, email)
	}
	return db.UsrUser{}, nil
}

func (m *mockQuerier) GetUserByID(ctx context.Context, id uuid.UUID) (db.UsrUser, error) {
	if m.getUserByID != nil {
		return m.getUserByID(ctx, id)
	}
	return db.UsrUser{}, nil
}

func (m *mockQuerier) GetUserByIDAny(ctx context.Context, id uuid.UUID) (db.UsrUser, error) {
	if m.getUserByIDAny != nil {
		return m.getUserByIDAny(ctx, id)
	}
	return db.UsrUser{}, nil
}

func (m *mockQuerier) GetUserByUsername(ctx context.Context, username string) (db.UsrUser, error) {
	if m.getUserByUsername != nil {
		return m.getUserByUsername(ctx, username)
	}
	return db.UsrUser{}, nil
}

func (m *mockQuerier) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]db.UsrRole, error) {
	if m.getUserRoles != nil {
		return m.getUserRoles(ctx, userID)
	}
	return nil, nil
}

func (m *mockQuerier) GetUsersByIDs(ctx context.Context, ids []uuid.UUID) ([]db.UsrUser, error) {
	if m.getUsersByIDs != nil {
		return m.getUsersByIDs(ctx, ids)
	}
	return nil, nil
}

func (m *mockQuerier) GetUsersByIDsAny(ctx context.Context, ids []uuid.UUID) ([]db.UsrUser, error) {
	if m.getUsersByIDsAny != nil {
		return m.getUsersByIDsAny(ctx, ids)
	}
	return nil, nil
}

func (m *mockQuerier) GetUsersByUsernames(ctx context.Context, usernames []string) ([]db.UsrUser, error) {
	if m.getUsersByUsernames != nil {
		return m.getUsersByUsernames(ctx, usernames)
	}
	return nil, nil
}

func (m *mockQuerier) IsFollowing(ctx context.Context, arg db.IsFollowingParams) (bool, error) {
	if m.isFollowing != nil {
		return m.isFollowing(ctx, arg)
	}
	return false, nil
}

func (m *mockQuerier) ListAllActiveUserIDs(ctx context.Context) ([]uuid.UUID, error) {
	if m.listAllActiveUserIDs != nil {
		return m.listAllActiveUserIDs(ctx)
	}
	return nil, nil
}

func (m *mockQuerier) ListRoles(ctx context.Context) ([]db.UsrRole, error) {
	if m.listRoles != nil {
		return m.listRoles(ctx)
	}
	return nil, nil
}

func (m *mockQuerier) MarkEmailTokenUsed(ctx context.Context, id uuid.UUID) error {
	if m.markEmailTokenUsed != nil {
		return m.markEmailTokenUsed(ctx, id)
	}
	return nil
}

func (m *mockQuerier) RemoveRoleFromUser(ctx context.Context, arg db.RemoveRoleFromUserParams) error {
	if m.removeRoleFromUser != nil {
		return m.removeRoleFromUser(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error {
	if m.revokeAllUserRefreshTokens != nil {
		return m.revokeAllUserRefreshTokens(ctx, userID)
	}
	return nil
}

func (m *mockQuerier) RevokeRefreshToken(ctx context.Context, token string) error {
	if m.revokeRefreshToken != nil {
		return m.revokeRefreshToken(ctx, token)
	}
	return nil
}

func (m *mockQuerier) SearchUsers(ctx context.Context, arg db.SearchUsersParams) ([]db.UsrUser, error) {
	if m.searchUsers != nil {
		return m.searchUsers(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) SearchUsersByQuery(ctx context.Context, arg db.SearchUsersByQueryParams) ([]db.UsrUser, error) {
	if m.searchUsersByQuery != nil {
		return m.searchUsersByQuery(ctx, arg)
	}
	return nil, nil
}

func (m *mockQuerier) Unfollow(ctx context.Context, arg db.UnfollowParams) error {
	if m.unfollow != nil {
		return m.unfollow(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) UpdateUser(ctx context.Context, arg db.UpdateUserParams) (db.UsrUser, error) {
	if m.updateUser != nil {
		return m.updateUser(ctx, arg)
	}
	return db.UsrUser{}, nil
}

func (m *mockQuerier) UpdateUserPassword(ctx context.Context, arg db.UpdateUserPasswordParams) error {
	if m.updateUserPassword != nil {
		return m.updateUserPassword(ctx, arg)
	}
	return nil
}

func (m *mockQuerier) UpdateUserProfile(ctx context.Context, arg db.UpdateUserProfileParams) (db.UsrUser, error) {
	if m.updateUserProfile != nil {
		return m.updateUserProfile(ctx, arg)
	}
	return db.UsrUser{}, nil
}
