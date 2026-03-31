package middleware

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// UngroupedAutoRoute resolves the target group for ungrouped API keys
// by inspecting the request body's "model" field and matching it to
// a platform-specific group the user has access to.
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

		platform := service.InferPlatformFromModel(modelVal)
		slog.Debug("ungrouped_auto_route",
			"model", modelVal,
			"inferred_platform", platform,
			"api_key_id", apiKey.ID)

		groups, err := groupRepo.ListActiveByPlatform(c.Request.Context(), platform)
		if err != nil {
			slog.Error("ungrouped_auto_route: failed to query groups", "error", err)
			writeError(c, http.StatusInternalServerError, "Failed to query groups")
			c.Abort()
			return
		}

		slog.Debug("ungrouped_auto_route",
			"candidate_groups", len(groups),
			"platform", platform)

		// Find the first group the user can access:
		// - non-subscription (v1 simplification: skip subscription-type groups)
		// - non-exclusive OR in user's AllowedGroups
		var resolved *service.Group
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
			resolved = g
			break
		}

		if resolved == nil {
			slog.Warn("ungrouped_auto_route: no group resolved",
				"model", modelVal, "platform", platform, "api_key_id", apiKey.ID)
			writeError(c, http.StatusForbidden,
				"No available group found for the requested model. Please contact the administrator.")
			c.Abort()
			return
		}

		slog.Info("ungrouped_auto_route: resolved",
			"model", modelVal,
			"platform", platform,
			"group_id", resolved.ID,
			"group_name", resolved.Name,
			"hydrated", resolved.Hydrated,
			"api_key_id", apiKey.ID)

		// Inject resolved group into the API key in context.
		apiKey.Group = resolved
		apiKey.GroupID = &resolved.ID
		c.Set(string(ContextKeyAPIKey), apiKey)
		setGroupContext(c, resolved)

		c.Next()
	}
}
