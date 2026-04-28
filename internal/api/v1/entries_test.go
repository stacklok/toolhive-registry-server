package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	upstreamv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/auth"
	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestPublishEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		body           []byte
		setupMock      func(*mocks.MockRegistryService)
		wantStatus     int
		wantError      string
		wantServerName string
	}{
		{
			name: "success - valid server data",
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"}, nil)
			},
			wantStatus:     http.StatusCreated,
			wantServerName: "test/server",
		},
		{
			name:       "invalid request body - not JSON",
			body:       []byte("not-json"),
			setupMock:  func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body:",
		},
		{
			name:       "neither server nor skill provided",
			body:       mustMarshal(publishEntryRequest{}),
			setupMock:  func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
			wantError:  "exactly one of 'server' or 'skill' must be provided",
		},
		{
			name: "version already exists",
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrVersionAlreadyExists)
			},
			wantStatus: http.StatusConflict,
			wantError:  "version already exists",
		},
		{
			name: "no managed source",
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, service.ErrNoManagedSource)
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "no managed source available for publishing",
		},
		{
			name: "invalid server name returns 400",
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "bad/name-", Version: "1.0.0"},
			}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("%w: bad/name-", service.ErrInvalidServerName))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid server name",
		},
		{
			name: "generic service error",
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("unexpected database error"))
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "failed to publish entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			router := Router(mockSvc, nil)
			req, err := http.NewRequest("POST", "/entries", bytes.NewReader(tt.body))
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response["error"], tt.wantError)
			}

			if tt.wantServerName != "" {
				var server upstreamv0.ServerJSON
				err = json.Unmarshal(rr.Body.Bytes(), &server)
				require.NoError(t, err)
				assert.Equal(t, tt.wantServerName, server.Name)
				assert.Equal(t, "1.0.0", server.Version)
			}
		})
	}
}

func TestPublishEntryClaimsRequired(t *testing.T) {
	t.Parallel()

	authzCfg := &config.AuthConfig{Mode: config.AuthModeOAuth, Authz: &config.AuthzConfig{}}
	oauthCfg := &config.AuthConfig{Mode: config.AuthModeOAuth}
	anonCfg := &config.AuthConfig{Mode: config.AuthModeAnonymous}

	tests := []struct {
		name       string
		authCfg    *config.AuthConfig
		body       []byte
		jwtClaims  jwt.MapClaims
		setupMock  func(*mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name:    "authz enabled without claims returns 400",
			authCfg: authzCfg,
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			jwtClaims:  jwt.MapClaims{"sub": "user1"},
			setupMock:  func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
			wantError:  "claims are required",
		},
		{
			name:    "authz enabled with empty claims returns 400",
			authCfg: authzCfg,
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
				Claims: map[string]any{},
			}),
			jwtClaims:  jwt.MapClaims{"sub": "user1"},
			setupMock:  func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
			wantError:  "claims are required",
		},
		{
			name:    "authz enabled with claims succeeds",
			authCfg: authzCfg,
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
				Claims: map[string]any{"sub": "user1"},
			}),
			jwtClaims: jwt.MapClaims{"sub": "user1"},
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:    "auth enabled without authz config without claims succeeds",
			authCfg: oauthCfg,
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			jwtClaims: jwt.MapClaims{"sub": "user1"},
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:    "anonymous mode without claims succeeds",
			authCfg: anonCfg,
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"}, nil)
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:    "no auth config without claims succeeds",
			authCfg: nil,
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().PublishServerVersion(gomock.Any(), gomock.Any()).
					Return(&upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"}, nil)
			},
			wantStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			router := Router(mockSvc, tt.authCfg)
			req, err := http.NewRequest("POST", "/entries", bytes.NewReader(tt.body))
			require.NoError(t, err)

			if tt.jwtClaims != nil {
				ctx := auth.ContextWithClaims(req.Context(), tt.jwtClaims)
				// Grant all roles so RequireRole middleware does not block
				// the request. This test focuses on the claims-required
				// check, not role enforcement.
				ctx = auth.ContextWithRoles(ctx, auth.AllRoles())
				req = req.WithContext(ctx)
			}

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response["error"], tt.wantError)
			}
		})
	}
}

func TestDeletePublishedEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMock  func(*mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "success - server type",
			path: "/entries/server/test%2Fserver/versions/1.0.0",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "success - skill type",
			path: "/entries/skill/test%2Fskill/versions/1.0.0",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteSkillVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "unsupported type",
			path:       "/entries/unknown/test%2Fentry/versions/1.0.0",
			setupMock:  func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
			wantError:  "unsupported entry type",
		},
		{
			name: "not found",
			path: "/entries/server/test%2Fserver/versions/1.0.0",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(service.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "no managed source",
			path: "/entries/server/test%2Fserver/versions/1.0.0",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(service.ErrNoManagedSource)
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "no managed source available for deletion",
		},
		{
			name: "generic service error",
			path: "/entries/server/test%2Fserver/versions/1.0.0",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().DeleteServerVersion(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("unexpected database error"))
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "failed to delete entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			router := Router(mockSvc, nil)
			req, err := http.NewRequest("DELETE", tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response["error"], tt.wantError)
			}

			if tt.wantStatus == http.StatusNoContent {
				assert.Empty(t, rr.Body.Bytes())
			}
		})
	}
}

func TestUpdateEntryClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		body       []byte
		setupMock  func(*mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "success - server type",
			path: "/entries/server/test%2Fserver/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{"org": "acme"}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "success - skill type",
			path: "/entries/skill/test%2Fskill/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{"org": "acme"}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "success - clear claims",
			path: "/entries/server/test%2Fserver/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name: "unsupported entry type",
			path: "/entries/unknown/test%2Fentry/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("invalid option: %w", service.ErrInvalidEntryType))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid entry type",
		},
		{
			name:       "invalid JSON body",
			path:       "/entries/server/test%2Fserver/claims",
			body:       []byte("not-json"),
			setupMock:  func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid request body",
		},
		{
			name: "entry not found",
			path: "/entries/server/test%2Fserver/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{"org": "acme"}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(service.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "claims insufficient",
			path: "/entries/server/test%2Fserver/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{"org": "acme"}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(service.ErrClaimsInsufficient)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "no managed source",
			path: "/entries/server/test%2Fserver/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{"org": "acme"}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(service.ErrNoManagedSource)
			},
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "no managed source available for updating claims",
		},
		{
			name: "generic service error",
			path: "/entries/server/test%2Fserver/claims",
			body: mustMarshal(map[string]any{"claims": map[string]any{"org": "acme"}}),
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().UpdateEntryClaims(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("unexpected error"))
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "failed to update entry claims",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			router := Router(mockSvc, nil)
			req, err := http.NewRequest(http.MethodPut, tt.path, bytes.NewReader(tt.body))
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response["error"], tt.wantError)
			}

			if tt.wantStatus == http.StatusNoContent {
				assert.Empty(t, rr.Body.Bytes())
			}
		})
	}
}

func TestGetEntryClaims(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		setupMock  func(*mocks.MockRegistryService)
		wantStatus int
		wantClaims map[string]any
		wantError  string
	}{
		{
			name: "success - server type",
			path: "/entries/server/test%2Fserver/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(map[string]any{"org": "acme", "team": "platform"}, nil)
			},
			wantStatus: http.StatusOK,
			wantClaims: map[string]any{"org": "acme", "team": "platform"},
		},
		{
			name: "success - skill type",
			path: "/entries/skill/test%2Fskill/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(map[string]any{"org": "acme"}, nil)
			},
			wantStatus: http.StatusOK,
			wantClaims: map[string]any{"org": "acme"},
		},
		{
			name: "success - empty claims returns empty object",
			path: "/entries/server/test%2Fserver/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(map[string]any{}, nil)
			},
			wantStatus: http.StatusOK,
			wantClaims: map[string]any{},
		},
		{
			name: "unsupported entry type from service",
			path: "/entries/server/test%2Fserver/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("invalid option: %w", service.ErrInvalidEntryType))
			},
			wantStatus: http.StatusBadRequest,
			wantError:  "invalid entry type",
		},
		{
			name: "entry not found",
			path: "/entries/server/test%2Fmissing/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, service.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "no managed source",
			path: "/entries/server/test%2Fserver/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, service.ErrNoManagedSource)
			},
			wantStatus: http.StatusServiceUnavailable,
			wantError:  "no managed source available",
		},
		{
			name: "claims insufficient",
			path: "/entries/server/test%2Fserver/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, service.ErrClaimsInsufficient)
			},
			wantStatus: http.StatusForbidden,
			wantError:  "insufficient claims",
		},
		{
			name: "generic service error",
			path: "/entries/server/test%2Fserver/claims",
			setupMock: func(m *mocks.MockRegistryService) {
				m.EXPECT().GetEntryClaims(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, fmt.Errorf("unexpected error"))
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "failed to get entry claims",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			tt.setupMock(mockSvc)

			router := Router(mockSvc, nil)
			req, err := http.NewRequest(http.MethodGet, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response["error"], tt.wantError)
			}

			if tt.wantStatus == http.StatusOK {
				var resp entryClaimsResponse
				err = json.Unmarshal(rr.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.NotNil(t, resp.Claims, "claims must be a non-nil JSON object")
				assert.Equal(t, tt.wantClaims, resp.Claims)
			}
		})
	}
}

// mustMarshal is a test helper that marshals v to JSON or panics.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
