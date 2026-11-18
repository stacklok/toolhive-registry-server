package api_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/stacklok/toolhive-registry-server/internal/api"
	"github.com/stacklok/toolhive-registry-server/internal/service/mocks"
)

// TestOpenAPIEndpoint_ConcurrentRequests reproduces a data race in
// openAPIHandler: swag.ReadDoc rewrites the registered Spec's Description on
// every call, so concurrent GET /openapi.json requests race. It fails under
// -race until the document is rendered once (or the call is serialized). It
// is not parallel so the race is reported against this test alone.
//
//nolint:paralleltest // see above
func TestOpenAPIEndpoint_ConcurrentRequests(t *testing.T) {
	svc := mocks.NewMockRegistryService(gomock.NewController(t))
	server, err := api.NewServer(svc, stubPages{})
	require.NoError(t, err)

	const workers = 8
	codes := make([]int, workers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
			codes[i] = rec.Code
		}()
	}
	close(start)
	wg.Wait()
	for _, code := range codes {
		assert.Equal(t, http.StatusOK, code)
	}
}
