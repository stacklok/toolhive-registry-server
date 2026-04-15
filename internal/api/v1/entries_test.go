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

	tests := []struct {
		name       string
		body       []byte
		jwtClaims  jwt.MapClaims
		setupMock  func(*mocks.MockRegistryService)
		wantStatus int
		wantError  string
	}{
		{
			name: "authenticated request without claims returns 400",
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			jwtClaims:  jwt.MapClaims{"sub": "user1"},
			setupMock:  func(_ *mocks.MockRegistryService) {},
			wantStatus: http.StatusBadRequest,
			wantError:  "claims are required",
		},
		{
			name: "authenticated request with claims succeeds",
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
			name: "unauthenticated request without claims succeeds",
			body: mustMarshal(publishEntryRequest{
				Server: &upstreamv0.ServerJSON{Name: "test/server", Version: "1.0.0"},
			}),
			jwtClaims: nil,
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

			router := Router(mockSvc, nil)
			req, err := http.NewRequest("POST", "/entries", bytes.NewReader(tt.body))
			require.NoError(t, err)

			if tt.jwtClaims != nil {
				ctx := auth.ContextWithClaims(req.Context(), tt.jwtClaims)
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

// mustMarshal is a test helper that marshals v to JSON or panics.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
