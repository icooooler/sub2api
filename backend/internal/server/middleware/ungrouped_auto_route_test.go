//go:build unit

package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ungroupedAutoRouteGroupRepoStub struct {
	groups []service.Group
	err    error
}

type ungroupedAutoRouteAccountRepoStub struct {
	accountsByGroupID map[int64][]service.Account
	errByGroupID      map[int64]error
}

type ungroupedAutoRouteCacheStub struct {
	stickyGroups map[string]int64
}

func (s *ungroupedAutoRouteAccountRepoStub) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error) {
	if err := s.errByGroupID[groupID]; err != nil {
		return nil, err
	}
	return s.accountsByGroupID[groupID], nil
}

func (s *ungroupedAutoRouteCacheStub) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, nil
}

func (s *ungroupedAutoRouteCacheStub) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (s *ungroupedAutoRouteCacheStub) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *ungroupedAutoRouteCacheStub) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (s *ungroupedAutoRouteCacheStub) GetStickyAutoRouteGroupID(_ context.Context, apiKeyID int64, sessionHash, modelKey string) (int64, error) {
	if s == nil || s.stickyGroups == nil {
		return 0, nil
	}
	return s.stickyGroups[stickyAutoRouteCacheKey(apiKeyID, sessionHash, modelKey)], nil
}

func (s *ungroupedAutoRouteCacheStub) SetStickyAutoRouteGroupID(_ context.Context, apiKeyID int64, sessionHash, modelKey string, groupID int64, _ time.Duration) error {
	if s == nil {
		return nil
	}
	if s.stickyGroups == nil {
		s.stickyGroups = map[string]int64{}
	}
	s.stickyGroups[stickyAutoRouteCacheKey(apiKeyID, sessionHash, modelKey)] = groupID
	return nil
}

func (s *ungroupedAutoRouteCacheStub) DeleteStickyAutoRouteGroupID(_ context.Context, apiKeyID int64, sessionHash, modelKey string) error {
	if s == nil || s.stickyGroups == nil {
		return nil
	}
	delete(s.stickyGroups, stickyAutoRouteCacheKey(apiKeyID, sessionHash, modelKey))
	return nil
}

func stickyAutoRouteCacheKey(apiKeyID int64, sessionHash, modelKey string) string {
	return fmt.Sprintf("%d:%s:%s", apiKeyID, sessionHash, modelKey)
}

func (s *ungroupedAutoRouteGroupRepoStub) GetByID(ctx context.Context, id int64) (*service.Group, error) {
	for i := range s.groups {
		if s.groups[i].ID == id {
			group := s.groups[i]
			return &group, nil
		}
	}
	return nil, service.ErrGroupNotFound
}

func (s *ungroupedAutoRouteGroupRepoStub) GetByIDLite(ctx context.Context, id int64) (*service.Group, error) {
	return s.GetByID(ctx, id)
}

func (s *ungroupedAutoRouteGroupRepoStub) List(ctx context.Context, params pagination.PaginationParams) ([]service.Group, *pagination.PaginationResult, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.groups, &pagination.PaginationResult{Page: 1, PageSize: len(s.groups), Total: int64(len(s.groups))}, nil
}

func (s *ungroupedAutoRouteGroupRepoStub) ListActive(ctx context.Context) ([]service.Group, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.groups, nil
}

func (s *ungroupedAutoRouteGroupRepoStub) ListActiveByPlatform(ctx context.Context, platform string) ([]service.Group, error) {
	if s.err != nil {
		return nil, s.err
	}
	result := make([]service.Group, 0, len(s.groups))
	for i := range s.groups {
		if s.groups[i].Platform == platform {
			result = append(result, s.groups[i])
		}
	}
	return result, nil
}

func (s *ungroupedAutoRouteGroupRepoStub) Create(ctx context.Context, group *service.Group) error {
	panic("unexpected call to Create")
}

func (s *ungroupedAutoRouteGroupRepoStub) Update(ctx context.Context, group *service.Group) error {
	panic("unexpected call to Update")
}

func (s *ungroupedAutoRouteGroupRepoStub) Delete(ctx context.Context, id int64) error {
	panic("unexpected call to Delete")
}

func (s *ungroupedAutoRouteGroupRepoStub) DeleteCascade(ctx context.Context, id int64) ([]int64, error) {
	panic("unexpected call to DeleteCascade")
}

func (s *ungroupedAutoRouteGroupRepoStub) ExistsByName(ctx context.Context, name string) (bool, error) {
	panic("unexpected call to ExistsByName")
}

func (s *ungroupedAutoRouteGroupRepoStub) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, status, search string, isExclusive *bool) ([]service.Group, *pagination.PaginationResult, error) {
	panic("unexpected call to ListWithFilters")
}

func (s *ungroupedAutoRouteGroupRepoStub) GetAccountCount(ctx context.Context, groupID int64) (int64, int64, error) {
	panic("unexpected call to GetAccountCount")
}

func (s *ungroupedAutoRouteGroupRepoStub) DeleteAccountGroupsByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected call to DeleteAccountGroupsByGroupID")
}

func (s *ungroupedAutoRouteGroupRepoStub) GetAccountIDsByGroupIDs(ctx context.Context, groupIDs []int64) ([]int64, error) {
	panic("unexpected call to GetAccountIDsByGroupIDs")
}

func (s *ungroupedAutoRouteGroupRepoStub) BindAccountsToGroup(ctx context.Context, groupID int64, accountIDs []int64) error {
	panic("unexpected call to BindAccountsToGroup")
}

func (s *ungroupedAutoRouteGroupRepoStub) UpdateSortOrders(ctx context.Context, updates []service.GroupSortOrderUpdate) error {
	panic("unexpected call to UpdateSortOrders")
}

func TestUngroupedAutoRoute_FallsBackToDefaultMappedModelWhenAccountRepoUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupRepo := &ungroupedAutoRouteGroupRepoStub{groups: []service.Group{
		{ID: 2, Name: "codex", Platform: service.PlatformOpenAI, DefaultMappedModel: "gpt-5.4", Status: service.StatusActive, Hydrated: true},
		{ID: 5, Name: "zhipu", Platform: service.PlatformOpenAI, DefaultMappedModel: "glm-5", Status: service.StatusActive, Hydrated: true},
		{ID: 1, Name: "claude", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
	}}

	user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive}
	apiKey := &service.APIKey{ID: 100, User: user, UserID: user.ID, Status: service.StatusActive}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Next()
	})
	r.Use(UngroupedAutoRoute(groupRepo, nil, nil, nil, AnthropicErrorWriter))
	r.POST("/t", func(c *gin.Context) {
		gotKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, gotKey.GroupID)
		require.Equal(t, int64(5), *gotKey.GroupID)
		require.NotNil(t, gotKey.Group)
		require.Equal(t, int64(5), gotKey.Group.ID)
		require.Nil(t, ConsumeNextAutoRouteGroup(c))
		c.Status(http.StatusOK)
	})

	body, err := json.Marshal(map[string]any{"model": "glm-5"})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestUngroupedAutoRoute_PrefersGroupWithSchedulableModelSupport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupRepo := &ungroupedAutoRouteGroupRepoStub{groups: []service.Group{
		{ID: 2, Name: "codex", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true},
		{ID: 5, Name: "zhipu", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
		{ID: 1, Name: "claude", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
	}}
	accountRepo := &ungroupedAutoRouteAccountRepoStub{accountsByGroupID: map[int64][]service.Account{
		2: {{ID: 20, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
		5: {{ID: 50, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"glm-5": "glm-5"}}}},
		1: {{ID: 10, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4-5": "claude-sonnet-4-5"}}}},
	}}

	user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive}
	apiKey := &service.APIKey{ID: 100, User: user, UserID: user.ID, Status: service.StatusActive}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Next()
	})
	r.Use(UngroupedAutoRoute(groupRepo, accountRepo, nil, nil, AnthropicErrorWriter))
	r.POST("/t", func(c *gin.Context) {
		gotKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, gotKey.GroupID)
		require.Equal(t, int64(5), *gotKey.GroupID)

		require.Nil(t, ConsumeNextAutoRouteGroup(c))
		c.Status(http.StatusOK)
	})

	body, err := json.Marshal(map[string]any{"model": "glm-5"})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestUngroupedAutoRoute_UsesWildcardModelSupport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupRepo := &ungroupedAutoRouteGroupRepoStub{groups: []service.Group{
		{ID: 2, Name: "codex", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true},
		{ID: 5, Name: "zhipu", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
	}}
	accountRepo := &ungroupedAutoRouteAccountRepoStub{accountsByGroupID: map[int64][]service.Account{
		2: {{ID: 20, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-*": "gpt-5.4"}}}},
		5: {{ID: 50, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"glm-*": "glm-5"}}}},
	}}

	user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive}
	apiKey := &service.APIKey{ID: 100, User: user, UserID: user.ID, Status: service.StatusActive}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Next()
	})
	r.Use(UngroupedAutoRoute(groupRepo, accountRepo, nil, nil, AnthropicErrorWriter))
	r.POST("/t", func(c *gin.Context) {
		gotKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, gotKey.GroupID)
		require.Equal(t, int64(5), *gotKey.GroupID)
		c.Status(http.StatusOK)
	})

	body, err := json.Marshal(map[string]any{"model": "glm-5-air"})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestUngroupedAutoRoute_PreservesFirstMatchingGroupAcrossPlatforms(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupRepo := &ungroupedAutoRouteGroupRepoStub{groups: []service.Group{
		{ID: 5, Name: "anthropic-first", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
		{ID: 2, Name: "openai-second", Platform: service.PlatformOpenAI, DefaultMappedModel: "gpt-5.4", Status: service.StatusActive, Hydrated: true},
		{ID: 9, Name: "other", Platform: service.PlatformGemini, Status: service.StatusActive, Hydrated: true},
	}}
	accountRepo := &ungroupedAutoRouteAccountRepoStub{accountsByGroupID: map[int64][]service.Account{
		5: {{ID: 50, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
		2: {{ID: 20, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
		9: {{ID: 90, Platform: service.PlatformGemini, Credentials: map[string]any{"model_mapping": map[string]any{"gemini-2.5-pro": "gemini-2.5-pro"}}}},
	}}

	user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive}
	apiKey := &service.APIKey{ID: 100, User: user, UserID: user.ID, Status: service.StatusActive}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), apiKey)
		c.Next()
	})
	r.Use(UngroupedAutoRoute(groupRepo, accountRepo, nil, nil, AnthropicErrorWriter))
	r.POST("/t", func(c *gin.Context) {
		gotKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, gotKey.GroupID)
		require.Equal(t, int64(5), *gotKey.GroupID)

		fallback := ConsumeNextAutoRouteGroup(c)
		require.NotNil(t, fallback)
		require.Equal(t, int64(2), fallback.ID)
		c.Status(http.StatusOK)
	})

	body, err := json.Marshal(map[string]any{"model": "gpt-5.4"})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestUngroupedAutoRoute_ReusesStickyGroupForSameSessionAndModelFamily(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupRepo := &ungroupedAutoRouteGroupRepoStub{groups: []service.Group{
		{ID: 5, Name: "anthropic-first", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
		{ID: 2, Name: "openai-second", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true},
	}}
	accountRepo := &ungroupedAutoRouteAccountRepoStub{accountsByGroupID: map[int64][]service.Account{
		5: {{ID: 50, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
		2: {{ID: 20, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
	}}
	cache := &ungroupedAutoRouteCacheStub{}

	user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive}
	apiKey := &service.APIKey{ID: 100, User: user, UserID: user.ID, Status: service.StatusActive}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		cloned := *apiKey
		cloned.GroupID = nil
		cloned.Group = nil
		c.Set(string(ContextKeyAPIKey), &cloned)
		c.Next()
	})
	r.Use(UngroupedAutoRoute(groupRepo, accountRepo, cache, nil, AnthropicErrorWriter))
	r.POST("/t", func(c *gin.Context) {
		gotKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, gotKey.GroupID)
		require.Equal(t, int64(5), *gotKey.GroupID)
		c.Status(http.StatusOK)
	})

	body, err := json.Marshal(map[string]any{
		"model":    "gpt-5.4",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	require.NoError(t, err)

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body))
	r.ServeHTTP(first, firstReq)
	require.Equal(t, http.StatusOK, first.Code)

	groupRepo.groups = []service.Group{
		{ID: 2, Name: "openai-second", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true},
		{ID: 5, Name: "anthropic-first", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
	}

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body))
	r.ServeHTTP(second, secondReq)
	require.Equal(t, http.StatusOK, second.Code)
}

func TestUngroupedAutoRoute_SeparatesStickyGroupsByModelFamily(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupRepo := &ungroupedAutoRouteGroupRepoStub{groups: []service.Group{
		{ID: 5, Name: "gpt-primary", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
		{ID: 2, Name: "gpt-secondary", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true},
		{ID: 9, Name: "glm-primary", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true},
	}}
	accountRepo := &ungroupedAutoRouteAccountRepoStub{accountsByGroupID: map[int64][]service.Account{
		5: {{ID: 50, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
		2: {{ID: 20, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
		9: {{ID: 90, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"glm-5": "glm-5"}}}},
	}}
	cache := &ungroupedAutoRouteCacheStub{}

	user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive}
	apiKey := &service.APIKey{ID: 100, User: user, UserID: user.ID, Status: service.StatusActive}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		cloned := *apiKey
		cloned.GroupID = nil
		cloned.Group = nil
		c.Set(string(ContextKeyAPIKey), &cloned)
		c.Next()
	})
	r.Use(UngroupedAutoRoute(groupRepo, accountRepo, cache, nil, AnthropicErrorWriter))
	r.POST("/t", func(c *gin.Context) {
		gotKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, gotKey.GroupID)
		c.Header("X-Group-ID", fmt.Sprintf("%d", *gotKey.GroupID))
		c.Status(http.StatusOK)
	})

	gptBody, err := json.Marshal(map[string]any{
		"model":    "gpt-5.4",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	require.NoError(t, err)
	glmBody, err := json.Marshal(map[string]any{
		"model":    "glm-5",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	require.NoError(t, err)

	gptResp := httptest.NewRecorder()
	r.ServeHTTP(gptResp, httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(gptBody)))
	require.Equal(t, http.StatusOK, gptResp.Code)

	require.Equal(t, "5", gptResp.Header().Get("X-Group-ID"))

	glmResp := httptest.NewRecorder()
	r.ServeHTTP(glmResp, httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(glmBody)))
	require.Equal(t, http.StatusOK, glmResp.Code)
	require.Equal(t, "9", glmResp.Header().Get("X-Group-ID"))
}

func TestUngroupedAutoRoute_BoundGroupKeyBypassesStickyRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupRepo := &ungroupedAutoRouteGroupRepoStub{groups: []service.Group{
		{ID: 5, Name: "ungrouped-target", Platform: service.PlatformAnthropic, Status: service.StatusActive, Hydrated: true},
		{ID: 2, Name: "bound-group", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true},
	}}
	accountRepo := &ungroupedAutoRouteAccountRepoStub{accountsByGroupID: map[int64][]service.Account{
		5: {{ID: 50, Platform: service.PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
		2: {{ID: 20, Platform: service.PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"}}}},
	}}
	cache := &ungroupedAutoRouteCacheStub{stickyGroups: map[string]int64{"preloaded": 5}}

	user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive}
	boundGroupID := int64(2)
	apiKey := &service.APIKey{ID: 100, User: user, UserID: user.ID, Status: service.StatusActive, GroupID: &boundGroupID, Group: &service.Group{ID: boundGroupID, Name: "bound-group", Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		cloned := *apiKey
		c.Set(string(ContextKeyAPIKey), &cloned)
		c.Next()
	})
	r.Use(UngroupedAutoRoute(groupRepo, accountRepo, cache, nil, AnthropicErrorWriter))
	r.POST("/t", func(c *gin.Context) {
		gotKey, ok := GetAPIKeyFromContext(c)
		require.True(t, ok)
		require.NotNil(t, gotKey.GroupID)
		require.Equal(t, int64(2), *gotKey.GroupID)
		require.Equal(t, int64(2), gotKey.Group.ID)
		require.Nil(t, ConsumeNextAutoRouteGroup(c))
		c.Status(http.StatusOK)
	})

	body, err := json.Marshal(map[string]any{
		"model":    "gpt-5.4",
		"messages": []map[string]any{{"role": "user", "content": "hello"}},
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", bytes.NewReader(body))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, map[string]int64{"preloaded": 5}, cache.stickyGroups)
}
