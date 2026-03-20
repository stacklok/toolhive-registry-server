package v1

import (
	"bytes"
	"encoding/json"
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

func TestV1StubEndpoints(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc, nil)

	tests := []struct {
		name      string
		method    string
		path      string
		wantError string
	}{
		// Endpoints still returning 501
		{
			name:      "list source entries",
			method:    "GET",
			path:      "/sources/my-source/entries",
			wantError: "Listing source entries is not yet implemented",
		},
		{
			name:      "list registry entries",
			method:    "GET",
			path:      "/registries/my-registry/entries",
			wantError: "Listing registry entries is not yet implemented",
		},
		{
			name:      "update entry claims",
			method:    "PUT",
			path:      "/entries/server/my-entry/claims",
			wantError: "Updating entry claims is not yet implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req, err := http.NewRequest(tt.method, tt.path, nil)
			require.NoError(t, err)

			rr := httptest.NewRecorder()
			router.ServeHTTP(rr, req)

			assert.Equal(t, http.StatusNotImplemented, rr.Code)

			var response map[string]string
			err = json.Unmarshal(rr.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, tt.wantError, response["error"])
		})
	}
}

func TestListSources(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().ListSources(gomock.Any()).Return([]service.SourceInfo{
		{Name: "src1", Type: "MANAGED", CreatedAt: now, UpdatedAt: now},
	}, nil)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("GET", "/sources", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp service.SourceListResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Len(t, resp.Sources, 1)
	assert.Equal(t, "src1", resp.Sources[0].Name)
}

func TestGetSource(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().GetSourceByName(gomock.Any(), "my-source").Return(
		&service.SourceInfo{Name: "my-source", Type: "MANAGED", CreatedAt: now, UpdatedAt: now}, nil)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("GET", "/sources/my-source", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp service.SourceInfo
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "my-source", resp.Name)
}

func TestGetSourceNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().GetSourceByName(gomock.Any(), "missing").Return(nil, service.ErrSourceNotFound)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("GET", "/sources/missing", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpsertSourceCreate(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().CreateSource(gomock.Any(), "new-source", gomock.Any()).Return(
		&service.SourceInfo{Name: "new-source", Type: "MANAGED", CreatedAt: now, UpdatedAt: now}, nil)

	router := Router(mockSvc, nil)
	body, _ := json.Marshal(service.SourceCreateRequest{})
	req, err := http.NewRequest("PUT", "/sources/new-source", bytes.NewReader(body))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestUpsertSourceUpdate(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().CreateSource(gomock.Any(), "existing", gomock.Any()).Return(nil, service.ErrSourceAlreadyExists)
	mockSvc.EXPECT().UpdateSource(gomock.Any(), "existing", gomock.Any()).Return(
		&service.SourceInfo{Name: "existing", Type: "MANAGED", CreatedAt: now, UpdatedAt: now}, nil)

	router := Router(mockSvc, nil)
	body, _ := json.Marshal(service.SourceCreateRequest{})
	req, err := http.NewRequest("PUT", "/sources/existing", bytes.NewReader(body))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestUpsertSourceBadBody(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc, nil)

	req, err := http.NewRequest("PUT", "/sources/my-source", bytes.NewReader([]byte("not-json")))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestDeleteSource(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().DeleteSource(gomock.Any(), "my-source").Return(nil)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("DELETE", "/sources/my-source", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestDeleteSourceNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().DeleteSource(gomock.Any(), "missing").Return(service.ErrSourceNotFound)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("DELETE", "/sources/missing", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeleteSourceInUse(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().DeleteSource(gomock.Any(), "busy").Return(service.ErrSourceInUse)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("DELETE", "/sources/busy", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusConflict, rr.Code)
}

func TestListRegistries(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().ListRegistries(gomock.Any()).Return([]service.RegistryInfo{
		{Name: "reg1", Sources: []string{"src1"}, CreatedAt: now, UpdatedAt: now},
	}, nil)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("GET", "/registries", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp registryListResponse
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	require.Len(t, resp.Registries, 1)
	assert.Equal(t, "reg1", resp.Registries[0].Name)
}

func TestGetRegistry(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().GetRegistryByName(gomock.Any(), "my-reg").Return(
		&service.RegistryInfo{Name: "my-reg", Sources: []string{"src1"}, CreatedAt: now, UpdatedAt: now}, nil)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("GET", "/registries/my-reg", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp service.RegistryInfo
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "my-reg", resp.Name)
}

func TestGetRegistryNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().GetRegistryByName(gomock.Any(), "missing").Return(nil, service.ErrRegistryNotFound)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("GET", "/registries/missing", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestUpsertRegistryCreate(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().CreateRegistry(gomock.Any(), "new-reg", gomock.Any()).Return(
		&service.RegistryInfo{Name: "new-reg", Sources: []string{"src1"}, CreatedAt: now, UpdatedAt: now}, nil)

	router := Router(mockSvc, nil)
	body, _ := json.Marshal(service.RegistryCreateRequest{Sources: []string{"src1"}})
	req, err := http.NewRequest("PUT", "/registries/new-reg", bytes.NewReader(body))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
}

func TestUpsertRegistryUpdate(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	now := time.Now()
	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().CreateRegistry(gomock.Any(), "existing", gomock.Any()).Return(nil, service.ErrRegistryAlreadyExists)
	mockSvc.EXPECT().UpdateRegistry(gomock.Any(), "existing", gomock.Any()).Return(
		&service.RegistryInfo{Name: "existing", Sources: []string{"src1"}, CreatedAt: now, UpdatedAt: now}, nil)

	router := Router(mockSvc, nil)
	body, _ := json.Marshal(service.RegistryCreateRequest{Sources: []string{"src1"}})
	req, err := http.NewRequest("PUT", "/registries/existing", bytes.NewReader(body))
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestDeleteRegistry(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().DeleteRegistry(gomock.Any(), "my-reg").Return(nil)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("DELETE", "/registries/my-reg", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestDeleteRegistryNotFound(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().DeleteRegistry(gomock.Any(), "missing").Return(service.ErrRegistryNotFound)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("DELETE", "/registries/missing", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeleteRegistryForbidden(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	mockSvc.EXPECT().DeleteRegistry(gomock.Any(), "config-reg").Return(service.ErrConfigRegistry)

	router := Router(mockSvc, nil)
	req, err := http.NewRequest("DELETE", "/registries/config-reg", nil)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

func TestV1URLParamValidation(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc, nil)

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantError  string
	}{
		// Source name validation - empty
		{
			name:       "get source - empty name",
			method:     "GET",
			path:       "/sources/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "upsert source - empty name",
			method:     "PUT",
			path:       "/sources/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "delete source - empty name",
			method:     "DELETE",
			path:       "/sources/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "list source entries - empty name",
			method:     "GET",
			path:       "/sources/%20/entries",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		// Source name validation - whitespace
		{
			name:       "get source - whitespace in name",
			method:     "GET",
			path:       "/sources/my%20source",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot contain whitespace",
		},
		{
			name:       "upsert source - whitespace in name",
			method:     "PUT",
			path:       "/sources/my%20source",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot contain whitespace",
		},
		{
			name:       "delete source - whitespace in name",
			method:     "DELETE",
			path:       "/sources/my%20source",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot contain whitespace",
		},
		{
			name:       "list source entries - whitespace in name",
			method:     "GET",
			path:       "/sources/my%20source/entries",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot contain whitespace",
		},
		// Registry name validation - empty
		{
			name:       "get registry - empty name",
			method:     "GET",
			path:       "/registries/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "upsert registry - empty name",
			method:     "PUT",
			path:       "/registries/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "delete registry - empty name",
			method:     "DELETE",
			path:       "/registries/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "list registry entries - empty name",
			method:     "GET",
			path:       "/registries/%20/entries",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		// Registry name validation - whitespace
		{
			name:       "get registry - whitespace in name",
			method:     "GET",
			path:       "/registries/my%09registry",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot contain whitespace",
		},
		// Entry param validation - empty type
		{
			name:       "delete published entry - empty type",
			method:     "DELETE",
			path:       "/entries/%20/my-entry/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
			wantError:  "type cannot be empty",
		},
		{
			name:       "update entry claims - empty type",
			method:     "PUT",
			path:       "/entries/%20/my-entry/claims",
			wantStatus: http.StatusBadRequest,
			wantError:  "type cannot be empty",
		},
		// Entry param validation - empty name
		{
			name:       "delete published entry - empty name",
			method:     "DELETE",
			path:       "/entries/server/%20/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "update entry claims - empty name",
			method:     "PUT",
			path:       "/entries/server/%20/claims",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		// Entry param validation - empty version
		{
			name:       "delete published entry - empty version",
			method:     "DELETE",
			path:       "/entries/server/my-entry/versions/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "version cannot be empty",
		},
		// Entry param validation - whitespace in type
		{
			name:       "delete published entry - whitespace in type",
			method:     "DELETE",
			path:       "/entries/my%20type/my-entry/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
			wantError:  "type cannot contain whitespace",
		},
		// Entry param validation - whitespace in name
		{
			name:       "delete published entry - whitespace in name",
			method:     "DELETE",
			path:       "/entries/server/my%20entry/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot contain whitespace",
		},
		// Entry param validation - whitespace in version
		{
			name:       "delete published entry - whitespace in version",
			method:     "DELETE",
			path:       "/entries/server/my-entry/versions/1.0%0A0",
			wantStatus: http.StatusBadRequest,
			wantError:  "version cannot contain whitespace",
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

			var response map[string]string
			err = json.Unmarshal(rr.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Equal(t, tt.wantError, response["error"])
		})
	}
}
