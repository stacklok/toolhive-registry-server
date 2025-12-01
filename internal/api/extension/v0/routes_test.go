package v0

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestUpsertVersion(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
		wantError  string
	}{
		{
			name:       "upsert version - valid ID",
			path:       "/registries/foo/servers/test-server-123/versions/1.0.0",
			method:     "PUT",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "upsert version - empty registry name",
			path:       "/registries/%20/servers/test-server-123/versions/1.0.0",
			method:     "PUT",
			wantStatus: http.StatusBadRequest,
			wantError:  "registryName cannot be empty",
		},
		{
			name:       "upsert version - empty server name",
			path:       "/registries/foo/servers/%20/versions/1.0.0",
			method:     "PUT",
			wantStatus: http.StatusBadRequest,
			wantError:  "serverName cannot be empty",
		},
		{
			name:       "upsert version - empty version",
			path:       "/registries/foo/servers/test-server-123/versions/%20",
			method:     "PUT",
			wantStatus: http.StatusBadRequest,
			wantError:  "version cannot be empty",
		},
		{
			name:       "upsert version - with special characters",
			path:       "/registries/foo/servers/test-server-123/versions/1.0%2F0.0",
			method:     "PUT",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "upsert version - no id",
			path:       "/registries/foo/servers/test-server-123/versions/",
			method:     "PUT",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusNotImplemented {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, "Creating or updating servers is not supported", response["error"])
			}

			if tt.wantStatus == http.StatusBadRequest && tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"])
			}
		})
	}
}

func TestURLEncodingInExtensionRoutes(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name        string
		path        string
		method      string
		wantStatus  int
		wantError   string
		description string
	}{
		// UpsertVersion endpoint tests
		{
			name:        "upsert version - URL encoded slash in registry name",
			path:        "/registries/test%2Fregistry/servers/my-server/versions/1.0.0",
			method:      "PUT",
			description: "Should decode test%2Fregistry to test/registry",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "upsert version - URL encoded slash in server name",
			path:        "/registries/foo/servers/test%2Fserver/versions/1.0.0",
			method:      "PUT",
			description: "Should decode test%2Fserver to test/server",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "upsert version - URL encoded slash in version",
			path:        "/registries/foo/servers/my-server/versions/1.0%2F0",
			method:      "PUT",
			description: "Should decode version with slash",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "upsert version - URL encoded at symbol",
			path:        "/registries/foo/servers/test%40v1/versions/1.0.0",
			method:      "PUT",
			description: "Should decode @ symbol in server name",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "upsert version - multiple URL encoded chars",
			path:        "/registries/test%2Fregistry/servers/server%40v1/versions/1.0%2B0",
			method:      "PUT",
			description: "Should decode multiple encoded characters",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "upsert version - URL encoded space in registry",
			path:        "/registries/test%20registry/servers/my-server/versions/1.0.0",
			method:      "PUT",
			description: "Should reject whitespace in registry",
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot contain whitespace",
		},
		{
			name:        "upsert version - URL encoded tab in server",
			path:        "/registries/foo/servers/test%09server/versions/1.0.0",
			method:      "PUT",
			description: "Should reject tab character in server",
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot contain whitespace",
		},
		{
			name:        "upsert version - URL encoded newline in version",
			path:        "/registries/foo/servers/my-server/versions/1.0%0A0",
			method:      "PUT",
			description: "Should reject newline in version",
			wantStatus:  http.StatusBadRequest,
			wantError:   "version cannot contain whitespace",
		},
		{
			name:        "upsert version - empty encoded registry",
			path:        "/registries/%20%20/servers/my-server/versions/1.0.0",
			method:      "PUT",
			description: "Should reject empty registry",
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot be empty",
		},
		{
			name:        "upsert version - empty encoded server",
			path:        "/registries/foo/servers/%09/versions/1.0.0",
			method:      "PUT",
			description: "Should reject empty server",
			wantStatus:  http.StatusBadRequest,
			wantError:   "serverName cannot be empty",
		},
		{
			name:        "upsert version - empty encoded version",
			path:        "/registries/foo/servers/my-server/versions/%0D",
			method:      "PUT",
			description: "Should reject empty version",
			wantStatus:  http.StatusBadRequest,
			wantError:   "version cannot be empty",
		},

		// UpsertRegistry endpoint tests
		{
			name:        "upsert registry - URL encoded slash",
			path:        "/registries/test%2Fregistry",
			method:      "PUT",
			description: "Should decode registry name with slash",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "upsert registry - URL encoded at symbol",
			path:        "/registries/test%40registry",
			method:      "PUT",
			description: "Should decode @ symbol",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "upsert registry - URL encoded space",
			path:        "/registries/test%20registry",
			method:      "PUT",
			description: "Should reject whitespace",
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot contain whitespace",
		},
		{
			name:        "upsert registry - empty encoded",
			path:        "/registries/%20%09",
			method:      "PUT",
			description: "Should reject empty registry",
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot be empty",
		},

		// DeleteRegistry endpoint tests
		{
			name:        "delete registry - URL encoded slash",
			path:        "/registries/test%2Fregistry",
			method:      "DELETE",
			description: "Should decode registry name",
			wantStatus:  http.StatusNotImplemented,
		},
		{
			name:        "delete registry - whitespace",
			path:        "/registries/test%0Aregistry",
			method:      "DELETE",
			description: "Should reject newline character",
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot contain whitespace",
		},
		{
			name:        "delete registry - empty",
			path:        "/registries/%20",
			method:      "DELETE",
			description: "Should reject empty registry",
			wantStatus:  http.StatusBadRequest,
			wantError:   "registryName cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code, "Status code mismatch for %s", tt.description)

			if tt.wantStatus == http.StatusBadRequest && tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"], "Error message mismatch")
			}

			if tt.wantStatus == http.StatusNotImplemented {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				// Verify we get the expected "not implemented" message
				assert.Contains(t, response["error"], "not supported", "Should return not supported message")
			}
		})
	}
}

func TestListRegistries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		method       string
		setupMock    func(*mocks.MockRegistryService)
		wantStatus   int
		wantError    string
		validateBody func(t *testing.T, body []byte)
	}{
		{
			name:   "list registries - valid request",
			path:   "/registries",
			method: "GET",
			setupMock: func(mockSvc *mocks.MockRegistryService) {
				mockSvc.EXPECT().
					ListRegistries(gomock.Any()).
					Return([]service.RegistryInfo{
						{
							Name:      "registry1",
							Type:      "MANAGED",
							CreatedAt: time.Now(),
							UpdatedAt: time.Now(),
						},
					}, nil)
			},
			wantStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				t.Helper()
				var response service.RegistryListResponse
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				assert.Len(t, response.Registries, 1)
				assert.Equal(t, "registry1", response.Registries[0].Name)
			},
		},
		{
			name:   "list registries - service error",
			path:   "/registries",
			method: "GET",
			setupMock: func(mockSvc *mocks.MockRegistryService) {
				mockSvc.EXPECT().
					ListRegistries(gomock.Any()).
					Return(nil, errors.New("database error"))
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "database error",
		},
		{
			name:   "list registries - not implemented",
			path:   "/registries",
			method: "GET",
			setupMock: func(mockSvc *mocks.MockRegistryService) {
				mockSvc.EXPECT().
					ListRegistries(gomock.Any()).
					Return(nil, service.ErrNotImplemented)
			},
			wantStatus: http.StatusNotImplemented,
			wantError:  "Listing registries is not supported in file mode",
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			router := Router(mockSvc)

			if tt.setupMock != nil {
				tt.setupMock(mockSvc)
			}

			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"])
			}

			if tt.validateBody != nil {
				tt.validateBody(t, rr.Body.Bytes())
			}
		})
	}
}

func TestGetRegistry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		method       string
		setupMock    func(*mocks.MockRegistryService)
		wantStatus   int
		wantError    string
		validateBody func(t *testing.T, body []byte)
	}{
		{
			name:   "get registry - valid name",
			path:   "/registries/foo",
			method: "GET",
			setupMock: func(mockSvc *mocks.MockRegistryService) {
				mockSvc.EXPECT().
					GetRegistryByName(gomock.Any(), "foo").
					Return(&service.RegistryInfo{
						Name:      "foo",
						Type:      "MANAGED",
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
					}, nil)
			},
			wantStatus: http.StatusOK,
			validateBody: func(t *testing.T, body []byte) {
				t.Helper()
				var response service.RegistryInfo
				err := json.Unmarshal(body, &response)
				require.NoError(t, err)
				assert.Equal(t, "foo", response.Name)
				assert.Equal(t, "MANAGED", response.Type)
			},
		},
		{
			name:   "get registry - not found",
			path:   "/registries/nonexistent",
			method: "GET",
			setupMock: func(mockSvc *mocks.MockRegistryService) {
				mockSvc.EXPECT().
					GetRegistryByName(gomock.Any(), "nonexistent").
					Return(nil, service.ErrRegistryNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantError:  "Registry nonexistent not found",
		},
		{
			name:       "get registry - empty registry name",
			path:       "/registries/%20",
			method:     "GET",
			wantStatus: http.StatusBadRequest,
			wantError:  "registryName cannot be empty",
		},
		{
			name:   "get registry - not implemented in file mode",
			path:   "/registries/foo",
			method: "GET",
			setupMock: func(mockSvc *mocks.MockRegistryService) {
				mockSvc.EXPECT().
					GetRegistryByName(gomock.Any(), "foo").
					Return(nil, service.ErrNotImplemented)
			},
			wantStatus: http.StatusNotImplemented,
			wantError:  "Getting registry is not supported in file mode",
		},
		{
			name:   "get registry - internal error",
			path:   "/registries/foo",
			method: "GET",
			setupMock: func(mockSvc *mocks.MockRegistryService) {
				mockSvc.EXPECT().
					GetRegistryByName(gomock.Any(), "foo").
					Return(nil, errors.New("database error"))
			},
			wantStatus: http.StatusInternalServerError,
			wantError:  "database error",
		},
		{
			name:       "get registry - no name",
			path:       "/registries/",
			method:     "GET",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		tt := tt // capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)

			mockSvc := mocks.NewMockRegistryService(ctrl)
			router := Router(mockSvc)

			if tt.setupMock != nil {
				tt.setupMock(mockSvc)
			}

			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"])
			}

			if tt.validateBody != nil {
				tt.validateBody(t, rr.Body.Bytes())
			}
		})
	}
}

func TestUpsertRegistry(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
		wantError  string
	}{
		{
			name:       "upsert registry - valid name",
			path:       "/registries/foo",
			method:     "PUT",
			wantStatus: http.StatusNotImplemented,
			wantError:  "Creating or updating registry is not supported",
		},
		{
			name:       "upsert registry - empty registry name",
			path:       "/registries/%20",
			method:     "PUT",
			wantStatus: http.StatusBadRequest,
			wantError:  "registryName cannot be empty",
		},
		{
			name:       "upsert registry - with special characters",
			path:       "/registries/foo%2Fbar",
			method:     "PUT",
			wantStatus: http.StatusNotImplemented,
			wantError:  "Creating or updating registry is not supported",
		},
		{
			name:       "upsert registry - no name",
			path:       "/registries/",
			method:     "PUT",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusNotImplemented {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"])
			}

			if tt.wantStatus == http.StatusBadRequest && tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"])
			}
		})
	}
}

func TestDeleteRegistry(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name       string
		path       string
		method     string
		wantStatus int
		wantError  string
	}{
		{
			name:       "delete registry - valid name",
			path:       "/registries/foo",
			method:     "DELETE",
			wantStatus: http.StatusNotImplemented,
			wantError:  "Deleting registry is not supported",
		},
		{
			name:       "delete registry - empty registry name",
			path:       "/registries/%20",
			method:     "DELETE",
			wantStatus: http.StatusBadRequest,
			wantError:  "registryName cannot be empty",
		},
		{
			name:       "delete registry - with special characters",
			path:       "/registries/foo%2Fbar",
			method:     "DELETE",
			wantStatus: http.StatusNotImplemented,
			wantError:  "Deleting registry is not supported",
		},
		{
			name:       "delete registry - no name",
			path:       "/registries/",
			method:     "DELETE",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantStatus, rr.Code)

			if tt.wantStatus == http.StatusNotImplemented {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"])
			}

			if tt.wantStatus == http.StatusBadRequest && tt.wantError != "" {
				var response map[string]string
				err = json.Unmarshal(rr.Body.Bytes(), &response)
				require.NoError(t, err)
				assert.Contains(t, response, "error")
				assert.Equal(t, tt.wantError, response["error"])
			}
		})
	}
}
