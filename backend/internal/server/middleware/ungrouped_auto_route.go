package middleware

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"sort"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

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

		// Use model name to infer platform, then query ALL active groups
		// and sort them so inferred-platform groups come first. This ensures
		// the primary group matches the correct handler while still allowing
		// cross-platform fallback for unknown model prefixes.
		platform := service.InferPlatformFromModel(modelVal)
		slog.Debug("ungrouped_auto_route",
			"inferred_platform", platform)

		allGroups, err := groupRepo.ListActive(c.Request.Context())
		if err != nil {
			slog.Error("ungrouped_auto_route: failed to query groups", "error", err)
			writeError(c, http.StatusInternalServerError, "Failed to query groups")
			c.Abort()
			return
		}

		candidates := filterCandidateGroups(allGroups, apiKey)
		sortCandidatesByPlatformPriority(candidates, platform)

		slog.Debug("ungrouped_auto_route",
			"candidate_groups", len(candidates),
			"platform", platform)

		if len(candidates) == 0 {
			slog.Warn("ungrouped_auto_route: no group resolved",
				"model", modelVal, "platform", platform, "api_key_id", apiKey.ID)
			writeError(c, http.StatusForbidden,
				"No available group found for the requested model. Please contact the administrator.")
			c.Abort()
			return
		}

		resolved := &candidates[0]

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
		if len(candidates) > 1 {
			fallbacks := candidates[1:]
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

// sortCandidatesByPlatformPriority sorts candidates so groups matching the
// inferred platform come first, preserving original order within each tier.
// This ensures the primary group picks the correct handler for route dispatch
// while still exposing other-platform groups as fallback candidates.
func sortCandidatesByPlatformPriority(candidates []service.Group, inferredPlatform string) {
	sort.SliceStable(candidates, func(i, j int) bool {
		iMatch := candidates[i].Platform == inferredPlatform
		jMatch := candidates[j].Platform == inferredPlatform
		if iMatch != jMatch {
			return iMatch
		}
		return false // preserve original order within same tier
	})
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
