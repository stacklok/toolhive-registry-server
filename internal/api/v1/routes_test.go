package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

func TestV1StubEndpoints(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

	tests := []struct {
		name      string
		method    string
		path      string
		wantError string
	}{
		// Source endpoints
		{
			name:      "list sources",
			method:    "GET",
			path:      "/sources",
			wantError: "Listing sources is not yet implemented",
		},
		{
			name:      "get source",
			method:    "GET",
			path:      "/sources/my-source",
			wantError: "Getting source is not yet implemented",
		},
		{
			name:      "upsert source",
			method:    "PUT",
			path:      "/sources/my-source",
			wantError: "Creating or updating source is not yet implemented",
		},
		{
			name:      "delete source",
			method:    "DELETE",
			path:      "/sources/my-source",
			wantError: "Deleting source is not yet implemented",
		},
		{
			name:      "list source entries",
			method:    "GET",
			path:      "/sources/my-source/entries",
			wantError: "Listing source entries is not yet implemented",
		},
		// Registry endpoints
		{
			name:      "list registries",
			method:    "GET",
			path:      "/registries",
			wantError: "Listing registries is not yet implemented",
		},
		{
			name:      "get registry",
			method:    "GET",
			path:      "/registries/my-registry",
			wantError: "Getting registry is not yet implemented",
		},
		{
			name:      "upsert registry",
			method:    "PUT",
			path:      "/registries/my-registry",
			wantError: "Creating or updating registry is not yet implemented",
		},
		{
			name:      "delete registry",
			method:    "DELETE",
			path:      "/registries/my-registry",
			wantError: "Deleting registry is not yet implemented",
		},
		{
			name:      "list registry entries",
			method:    "GET",
			path:      "/registries/my-registry/entries",
			wantError: "Listing registry entries is not yet implemented",
		},
		// Publish endpoints
		{
			name:      "publish entry",
			method:    "POST",
			path:      "/publish",
			wantError: "Publishing entry is not yet implemented",
		},
		{
			name:      "delete published entry",
			method:    "DELETE",
			path:      "/publish/my-entry/versions/1.0.0",
			wantError: "Deleting published entry is not yet implemented",
		},
		{
			name:      "update entry claims",
			method:    "PUT",
			path:      "/publish/my-entry/versions/1.0.0/claims",
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

func TestV1URLParamValidation(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockSvc := mocks.NewMockRegistryService(ctrl)
	router := Router(mockSvc)

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
		// Publish param validation - empty name
		{
			name:       "delete published entry - empty name",
			method:     "DELETE",
			path:       "/publish/%20/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		{
			name:       "update entry claims - empty name",
			method:     "PUT",
			path:       "/publish/%20/versions/1.0.0/claims",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot be empty",
		},
		// Publish param validation - empty version
		{
			name:       "delete published entry - empty version",
			method:     "DELETE",
			path:       "/publish/my-entry/versions/%20",
			wantStatus: http.StatusBadRequest,
			wantError:  "version cannot be empty",
		},
		{
			name:       "update entry claims - empty version",
			method:     "PUT",
			path:       "/publish/my-entry/versions/%20/claims",
			wantStatus: http.StatusBadRequest,
			wantError:  "version cannot be empty",
		},
		// Publish param validation - whitespace in name
		{
			name:       "delete published entry - whitespace in name",
			method:     "DELETE",
			path:       "/publish/my%20entry/versions/1.0.0",
			wantStatus: http.StatusBadRequest,
			wantError:  "name cannot contain whitespace",
		},
		// Publish param validation - whitespace in version
		{
			name:       "update entry claims - whitespace in version",
			method:     "PUT",
			path:       "/publish/my-entry/versions/1.0%0A0/claims",
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
