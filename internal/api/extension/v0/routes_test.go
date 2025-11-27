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
			wantError:  "Registry name is required",
		},
		{
			name:       "upsert version - empty server name",
			path:       "/registries/foo/servers/%20/versions/1.0.0",
			method:     "PUT",
			wantStatus: http.StatusBadRequest,
			wantError:  "Server ID is required",
		},
		{
			name:       "upsert version - empty version",
			path:       "/registries/foo/servers/test-server-123/versions/%20",
			method:     "PUT",
			wantStatus: http.StatusBadRequest,
			wantError:  "Version is required",
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

func TestListRegistries(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name         string
		path         string
		method       string
		setupMock    func()
		wantStatus   int
		wantError    string
		validateBody func(t *testing.T, body []byte)
	}{
		{
			name:   "list registries - valid request",
			path:   "/registries",
			method: "GET",
			setupMock: func() {
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
			setupMock: func() {
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
			setupMock: func() {
				mockSvc.EXPECT().
					ListRegistries(gomock.Any()).
					Return(nil, service.ErrNotImplemented)
			},
			wantStatus: http.StatusNotImplemented,
			wantError:  "Listing registries is not supported in file mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.setupMock != nil {
				tt.setupMock()
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
			name:       "get registry - valid name",
			path:       "/registries/foo",
			method:     "GET",
			wantStatus: http.StatusNotImplemented,
			wantError:  "Getting registry is not supported",
		},
		{
			name:       "get registry - empty registry name",
			path:       "/registries/%20",
			method:     "GET",
			wantStatus: http.StatusBadRequest,
			wantError:  "Registry name is required",
		},
		{
			name:       "get registry - with special characters",
			path:       "/registries/foo%2Fbar",
			method:     "GET",
			wantStatus: http.StatusNotImplemented,
			wantError:  "Getting registry is not supported",
		},
		{
			name:       "get registry - no name",
			path:       "/registries/",
			method:     "GET",
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
			wantError:  "Registry name is required",
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
			wantError:  "Registry name is required",
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
