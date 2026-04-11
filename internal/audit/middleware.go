package audit

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/stacklok/toolhive-core/audit"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
)

// Middleware returns HTTP middleware that emits audit events for operations
// on /v1/ endpoints. It must be installed after auth and role resolution
// middleware so that JWT claims are available in the context.
//
// When cfg is nil or disabled, the middleware is a no-op pass-through.
func Middleware(cfg *config.AuditConfig, logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if cfg == nil || !cfg.Enabled || logger == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Inject a mutable carrier so Audited* wrappers can record RouteInfo.
			ctx := newRouteInfoCarrier(r.Context())
			r = r.WithContext(ctx)

			// Capture request body if configured.
			var reqBody []byte
			if cfg.IncludeRequestData && isMutating(r.Method) {
				reqBody = captureRequestBody(r, cfg.GetMaxDataSize())
			}

			// Wrap the response writer to capture status and bytes.
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			// Serve the request so we can observe the outcome.
			next.ServeHTTP(ww, r)

			duration := time.Since(start)

			// Read RouteInfo injected by the Audited* wrapper for this route.
			// If nil, the route is not annotated — skip auditing.
			info := RouteInfoFromContext(r.Context())
			if info == nil {
				return
			}

			// Resolve event type: upsert handlers choose based on HTTP status.
			var eventType string
			if info.OnCreate != "" {
				if ww.Status() == http.StatusCreated {
					eventType = info.OnCreate
				} else {
					eventType = info.OnUpdate
				}
			} else {
				eventType = info.EventType
			}
			if eventType == "" {
				return
			}

			// Apply event type filtering.
			if !isEventAllowed(eventType, cfg.EventTypes, cfg.ExcludeEventTypes) {
				return
			}

			emitEvent(r, ww.Status(), ww.BytesWritten(), duration, eventType, info.Target, reqBody, logger)
		})
	}
}

// AuthFailureMiddleware returns HTTP middleware that emits audit events for
// authentication failures (HTTP 401). It must be installed BEFORE the auth
// middleware so it can observe auth rejections.
//
// When cfg is nil or disabled, the middleware is a no-op pass-through.
func AuthFailureMiddleware(cfg *config.AuditConfig, logger *Logger, publicPaths []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if cfg == nil || !cfg.Enabled || logger == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth.IsPublicPath(r.URL.Path, publicPaths) {
				next.ServeHTTP(w, r)
				return
			}

			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			if ww.Status() == http.StatusUnauthorized {
				if isEventAllowed(EventAuthUnauthenticated, cfg.EventTypes, cfg.ExcludeEventTypes) {
					emitAuthFailureEvent(r, logger)
				}
			}
		})
	}
}

// isMutating returns true for methods that modify state.
func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// isEventAllowed checks whether an event type passes the configured
// whitelist/blacklist filters. An empty whitelist allows all events.
// The blacklist takes precedence.
func isEventAllowed(eventType string, whitelist, blacklist []string) bool {
	for _, excluded := range blacklist {
		if excluded == eventType {
			return false
		}
	}
	if len(whitelist) == 0 {
		return true
	}
	for _, allowed := range whitelist {
		if allowed == eventType {
			return true
		}
	}
	return false
}

// captureRequestBody reads and returns up to maxSize bytes of the request
// body, then replaces r.Body so downstream handlers can still read it.
// The remaining body is NOT buffered into memory — it streams lazily from
// the original reader when downstream handlers consume it.
func captureRequestBody(r *http.Request, maxSize int) []byte {
	if r.Body == nil {
		return nil
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, int64(maxSize)))
	if err != nil || len(buf) == 0 {
		return nil
	}
	// Restore the body: captured prefix + remaining original body (streamed lazily).
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), r.Body))
	return buf
}

// emitEvent builds and logs an audit event.
func emitEvent(
	r *http.Request,
	status int,
	bytesWritten int,
	duration time.Duration,
	eventType string,
	target map[string]string,
	reqBody []byte,
	logger *Logger,
) {
	source := SourceFromRequest(r)
	outcome := OutcomeFromStatus(status)
	subjects := subjectsFromRequest(r)

	event := audit.NewAuditEvent(
		eventType,
		source,
		outcome,
		subjects,
		ComponentRegistryAPI,
	).WithTarget(target)

	// Metadata: request ID, duration, response size.
	extra := map[string]any{
		"duration_ms":    duration.Milliseconds(),
		"response_bytes": bytesWritten,
	}
	if reqID := middleware.GetReqID(r.Context()); reqID != "" {
		extra["request_id"] = reqID
	}
	event.Metadata.Extra = extra

	// Attach request body data if captured.
	if len(reqBody) > 0 {
		if json.Valid(reqBody) {
			raw := json.RawMessage(reqBody)
			event = event.WithData(&raw)
		} else {
			// Safely escape non-JSON body content to prevent log injection.
			escaped, marshalErr := json.Marshal(string(reqBody))
			if marshalErr == nil {
				raw := json.RawMessage(escaped)
				event = event.WithData(&raw)
			}
		}
	}

	event.LogTo(r.Context(), logger.Slog(), auditLevel)
}

// emitAuthFailureEvent builds an audit event for a failed authentication attempt.
func emitAuthFailureEvent(r *http.Request, logger *Logger) {
	source := SourceFromRequest(r)

	target := map[string]string{
		"method": r.Method,
		"path":   r.URL.Path,
	}

	event := audit.NewAuditEvent(
		EventAuthUnauthenticated,
		source,
		audit.OutcomeDenied,
		map[string]string{"identity": "unknown"},
		ComponentRegistryAPI,
	).WithTarget(target)

	if reqID := middleware.GetReqID(r.Context()); reqID != "" {
		event.Metadata.Extra = map[string]any{
			"request_id": reqID,
		}
	}

	event.LogTo(r.Context(), logger.Slog(), auditLevel)
}

// subjectsFromRequest extracts the authenticated subject from JWT claims.
// Follows toolhive's fallback order: sub, then name/preferred_username/email
// for display name. Returns an "anonymous" marker when no claims are present.
func subjectsFromRequest(r *http.Request) map[string]string {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		return map[string]string{"identity": "anonymous"}
	}

	subjects := make(map[string]string, 4)

	if sub, ok := claims["sub"].(string); ok && sub != "" {
		subjects["sub"] = sub
	} else {
		subjects["identity"] = "anonymous"
	}

	// Display name: name → preferred_username → email (toolhive parity)
	if name := claimString(claims, "name"); name != "" {
		subjects["user"] = name
	} else if pref := claimString(claims, "preferred_username"); pref != "" {
		subjects["user"] = pref
	} else if email := claimString(claims, "email"); email != "" {
		subjects["user"] = email
	}

	if auth.IsSuperAdmin(r.Context()) {
		subjects["role"] = "super_admin"
	}

	return subjects
}

// claimString extracts a string claim value, returning "" if missing or wrong type.
func claimString(claims map[string]any, key string) string {
	v, ok := claims[key].(string)
	if !ok {
		return ""
	}
	return v
}
