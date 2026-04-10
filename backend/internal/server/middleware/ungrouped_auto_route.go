package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/tidwall/gjson"
)

type groupSchedulableAccountLister interface {
	ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]service.Account, error)
}

const stickyAutoRouteTTL = time.Hour

// UngroupedAutoRoute resolves the target group for ungrouped API keys
// by inspecting the request body's "model" field and matching it to
// an active group the user has access to.
//
// When multiple candidate groups exist (e.g. two openai-type groups for
// different providers), the first candidate is set as the current group
// and the remaining candidates are stored in context so that downstream
// handlers can fall back to the next group if no accounts are found.
//
// Prerequisite: must run AFTER apiKeyAuth and RequireGroupAssignment.
// When the key already has a group, this middleware is a no-op.
func UngroupedAutoRoute(
	groupRepo service.GroupRepository,
	accountRepo groupSchedulableAccountLister,
	cache service.GatewayCache,
	settingService *service.SettingService,
	writeError GatewayErrorWriter,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey, ok := GetAPIKeyFromContext(c)
		if !ok || apiKey.GroupID != nil {
			c.Next()
			return
		}

		// GET requests (e.g. /v1/models, /v1/usage) have no body to inspect.
		if c.Request.Method == http.MethodGet {
			c.Next()
			return
		}

		// Read body bytes so we can peek at "model" and then restore the reader.
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "Failed to read request body")
			c.Abort()
			return
		}
		// Always restore body for downstream handlers.
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		modelVal := gjson.GetBytes(body, "model").String()
		if modelVal == "" {
			// No model field — let downstream handler deal with the validation.
			c.Next()
			return
		}

		slog.Debug("ungrouped_auto_route",
			"model", modelVal,
			"api_key_id", apiKey.ID)

		allGroups, err := groupRepo.ListActive(c.Request.Context())
		if err != nil {
			slog.Error("ungrouped_auto_route: failed to query groups", "error", err)
			writeError(c, http.StatusInternalServerError, "Failed to query groups")
			c.Abort()
			return
		}

		candidates := filterCandidateGroups(allGroups, apiKey)
		candidates = sortAndFilterCandidatesForModel(c.Request.Context(), candidates, modelVal, accountRepo)

		resolved, fallbacks := resolveStickyAutoRouteGroup(c.Request.Context(), apiKey, modelVal, body, candidates, cache)

		slog.Debug("ungrouped_auto_route",
			"candidate_groups", len(candidates),
			"model", modelVal)

		if resolved == nil {
			slog.Warn("ungrouped_auto_route: no group resolved",
				"model", modelVal, "api_key_id", apiKey.ID)
			writeError(c, http.StatusForbidden,
				"No available group found for the requested model. Please contact the administrator.")
			c.Abort()
			return
		}

		slog.Info("ungrouped_auto_route: resolved",
			"model", modelVal,
			"platform", resolved.Platform,
			"group_id", resolved.ID,
			"group_name", resolved.Name,
			"hydrated", resolved.Hydrated,
			"candidate_count", len(candidates),
			"api_key_id", apiKey.ID)

		// Inject resolved group into the API key in context.
		apiKey.Group = resolved
		apiKey.GroupID = &resolved.ID
		c.Set(string(ContextKeyAPIKey), apiKey)
		setGroupContext(c, resolved)

		// Store remaining candidates for downstream fallback.
		if len(fallbacks) > 0 {
			ctx := context.WithValue(c.Request.Context(), ctxkey.AutoRouteFallbackGroups, fallbacks)
			c.Request = c.Request.WithContext(ctx)
		}

		c.Next()
	}
}

// filterCandidateGroups returns groups the user can access:
// non-subscription and non-exclusive (or in AllowedGroups).
func filterCandidateGroups(groups []service.Group, apiKey *service.APIKey) []service.Group {
	candidates := make([]service.Group, 0, len(groups))
	for i := range groups {
		g := &groups[i]
		if g.IsSubscriptionType() {
			slog.Debug("ungrouped_auto_route: skip subscription group",
				"group_id", g.ID, "group_name", g.Name)
			continue
		}
		if apiKey.User != nil && !apiKey.User.CanBindGroup(g.ID, g.IsExclusive) {
			slog.Debug("ungrouped_auto_route: user cannot bind group",
				"group_id", g.ID, "group_name", g.Name, "is_exclusive", g.IsExclusive)
			continue
		}
		candidates = append(candidates, *g)
	}
	return candidates
}

// sortAndFilterCandidatesForModel keeps only groups that can actually serve the
// requested model while preserving the original candidate ordering.
//
// When accountRepo is available, a group is considered routable only if it has
// at least one schedulable account supporting the requested model. This avoids
// selecting a group solely because its default mapping matches while no account
// in that group can really handle the request.
//
// When accountRepo is unavailable, fall back to an exact DefaultMappedModel
// match so middleware-only tests and lightweight callers still have a minimal
// routing signal.
func sortAndFilterCandidatesForModel(ctx context.Context, candidates []service.Group, requestedModel string, accountRepo groupSchedulableAccountLister) []service.Group {
	filtered := make([]service.Group, 0, len(candidates))
	for _, g := range candidates {
		supportsModel := false
		if accountRepo != nil {
			supportsModel = groupSupportsModel(ctx, accountRepo, g.ID, requestedModel)
		} else if g.DefaultMappedModel == requestedModel {
			supportsModel = true
		}
		if supportsModel {
			filtered = append(filtered, g)
			continue
		}
		slog.Debug("ungrouped_auto_route: skip group without model support",
			"group_id", g.ID, "group_name", g.Name, "model", requestedModel)
	}
	return filtered
}

func groupSupportsModel(ctx context.Context, accountRepo groupSchedulableAccountLister, groupID int64, requestedModel string) bool {
	if accountRepo == nil || requestedModel == "" {
		return false
	}

	accounts, err := accountRepo.ListSchedulableByGroupID(ctx, groupID)
	if err != nil {
		slog.Warn("ungrouped_auto_route: failed to query schedulable accounts",
			"group_id", groupID,
			"model", requestedModel,
			"error", err)
		return false
	}

	for i := range accounts {
		if accounts[i].IsModelSupported(requestedModel) {
			return true
		}
	}
	return false
}

func resolveStickyAutoRouteGroup(ctx context.Context, apiKey *service.APIKey, requestedModel string, body []byte, candidates []service.Group, cache service.GatewayCache) (*service.Group, []service.Group) {
	if len(candidates) == 0 {
		return nil, nil
	}

	sessionHash, modelKey := buildStickyAutoRouteKeys(apiKey, requestedModel, body)
	if cache != nil && sessionHash != "" && modelKey != "" {
		stickyGroupID, err := cache.GetStickyAutoRouteGroupID(ctx, apiKey.ID, sessionHash, modelKey)
		if err == nil && stickyGroupID > 0 {
			if resolved, fallbacks, ok := pickStickyCandidate(candidates, stickyGroupID); ok {
				return resolved, fallbacks
			}
			if delErr := cache.DeleteStickyAutoRouteGroupID(ctx, apiKey.ID, sessionHash, modelKey); delErr != nil {
				slog.Warn("ungrouped_auto_route: failed to delete stale sticky group binding",
					"api_key_id", apiKey.ID,
					"model", requestedModel,
					"error", delErr)
			}
		} else if err != nil && !errors.Is(err, redis.Nil) {
			slog.Warn("ungrouped_auto_route: failed to read sticky group binding",
				"api_key_id", apiKey.ID,
				"model", requestedModel,
				"error", err)
		}
	}

	resolved := &candidates[0]
	fallbacks := candidates[1:]
	if cache != nil && sessionHash != "" && modelKey != "" {
		if err := cache.SetStickyAutoRouteGroupID(ctx, apiKey.ID, sessionHash, modelKey, resolved.ID, stickyAutoRouteTTL); err != nil {
			slog.Warn("ungrouped_auto_route: failed to persist sticky group binding",
				"api_key_id", apiKey.ID,
				"group_id", resolved.ID,
				"model", requestedModel,
				"error", err)
		}
	}
	return resolved, fallbacks
}

func buildStickyAutoRouteKeys(apiKey *service.APIKey, requestedModel string, body []byte) (string, string) {
	if apiKey == nil || apiKey.ID <= 0 {
		return "", ""
	}
	trimmedModel := strings.TrimSpace(requestedModel)
	if trimmedModel == "" {
		return "", ""
	}

	parsedReq, err := service.ParseGatewayRequest(body, inferStickyAutoRouteProtocol(trimmedModel))
	if err != nil || parsedReq == nil {
		parsedReq = &service.ParsedRequest{Model: trimmedModel, Body: body}
	}
	parsedReq.SessionContext = &service.SessionContext{APIKeyID: apiKey.ID}

	sessionHash := (&service.GatewayService{}).GenerateSessionHash(parsedReq)
	if sessionHash == "" {
		return "", ""
	}
	return sessionHash, stickyAutoRouteModelKey(trimmedModel)
}

func inferStickyAutoRouteProtocol(model string) string {
	if service.InferPlatformFromModel(model) == service.PlatformGemini {
		return domain.PlatformGemini
	}
	return domain.PlatformAnthropic
}

func stickyAutoRouteModelKey(model string) string {
	trimmed := strings.TrimSpace(strings.ToLower(model))
	if trimmed == "" {
		return ""
	}
	if normalized := service.NormalizeOpenAICompatRequestedModel(trimmed); normalized != "" {
		trimmed = normalized
	}
	trimmed = strings.TrimPrefix(trimmed, "models/")
	if idx := strings.LastIndex(trimmed, "/models/"); idx != -1 {
		trimmed = trimmed[idx+len("/models/"):]
	}
	parts := strings.Split(trimmed, "-")
	if len(parts) >= 2 {
		return parts[0] + "-" + parts[1]
	}
	return trimmed
}

func pickStickyCandidate(candidates []service.Group, stickyGroupID int64) (*service.Group, []service.Group, bool) {
	if stickyGroupID <= 0 {
		return nil, nil, false
	}
	for i := range candidates {
		if candidates[i].ID != stickyGroupID {
			continue
		}
		resolved := &candidates[i]
		fallbacks := make([]service.Group, 0, len(candidates)-1)
		fallbacks = append(fallbacks, candidates[:i]...)
		fallbacks = append(fallbacks, candidates[i+1:]...)
		return resolved, fallbacks, true
	}
	return nil, nil, false
}

// ConsumeNextAutoRouteGroup pops the next fallback group from the context.
// Returns nil if no more candidates are available.
// When a group is returned, it also updates the API key and group context.
func ConsumeNextAutoRouteGroup(c *gin.Context) *service.Group {
	fallbacks, ok := c.Request.Context().Value(ctxkey.AutoRouteFallbackGroups).([]service.Group)
	if !ok || len(fallbacks) == 0 {
		return nil
	}

	next := &fallbacks[0]
	remaining := fallbacks[1:]

	// Update context with remaining fallbacks.
	var ctx context.Context
	if len(remaining) > 0 {
		ctx = context.WithValue(c.Request.Context(), ctxkey.AutoRouteFallbackGroups, remaining)
	} else {
		ctx = context.WithValue(c.Request.Context(), ctxkey.AutoRouteFallbackGroups, ([]service.Group)(nil))
	}
	c.Request = c.Request.WithContext(ctx)

	// Update the API key's group binding.
	apiKey, ok := GetAPIKeyFromContext(c)
	if ok {
		apiKey.Group = next
		apiKey.GroupID = &next.ID
		c.Set(string(ContextKeyAPIKey), apiKey)
		setGroupContext(c, next)
	}

	slog.Info("ungrouped_auto_route: fallback to next group",
		"group_id", next.ID,
		"group_name", next.Name,
		"remaining_candidates", len(remaining))

	return next
}
