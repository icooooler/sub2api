//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminService_UpdateUser_AllowedGroupsChange_InvalidatesAuthCache(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 7, AllowedGroups: []int64{1, 2}, Status: StatusActive, Role: RoleUser, Concurrency: 1}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		authCacheInvalidator: invalidator,
	}

	allowedGroups := []int64{1, 3}
	updated, err := svc.UpdateUser(context.Background(), 7, &UpdateUserInput{AllowedGroups: &allowedGroups})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, updated.AllowedGroups)
	require.Equal(t, []int64{7}, invalidator.userIDs)
}

func TestAdminService_UpdateUser_AllowedGroupsUnchanged_DoesNotInvalidateAuthCache(t *testing.T) {
	baseRepo := &userRepoStub{user: &User{ID: 8, AllowedGroups: []int64{1, 2}, Status: StatusActive, Role: RoleUser, Concurrency: 1}}
	repo := &balanceUserRepoStub{userRepoStub: baseRepo}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		authCacheInvalidator: invalidator,
	}

	allowedGroups := []int64{1, 2}
	updated, err := svc.UpdateUser(context.Background(), 8, &UpdateUserInput{AllowedGroups: &allowedGroups})
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, updated.AllowedGroups)
	require.Empty(t, invalidator.userIDs)
}
