package v0

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

func TestUpdateServer(t *testing.T) {
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
	}{
		{
			name:       "update server - valid ID",
			path:       "/servers/test-server-123",
			method:     "PUT",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "update server - empty ID",
			path:       "/servers/",
			method:     "PUT",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "update server - with special characters",
			path:       "/servers/test-server%2Fwith-slash",
			method:     "PUT",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "update server - no id",
			path:       "/servers/%20",
			method:     "PUT",
			wantStatus: http.StatusBadRequest,
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
				assert.Equal(t, "Updating servers is not supported", response["error"])
			}
		})
	}
}

func TestDeleteServer(t *testing.T) {
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
	}{
		{
			name:       "delete server - valid ID",
			path:       "/servers/test-server-123",
			method:     "DELETE",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "delete server - empty ID",
			path:       "/servers/",
			method:     "DELETE",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "delete server - with special characters",
			path:       "/servers/test-server%2Fwith-slash",
			method:     "DELETE",
			wantStatus: http.StatusNotImplemented,
		},
		{
			name:       "update server - no id",
			path:       "/servers/%20",
			method:     "DELETE",
			wantStatus: http.StatusBadRequest,
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
				assert.Equal(t, "Deleting servers is not supported", response["error"])
			}
		})
	}
}
