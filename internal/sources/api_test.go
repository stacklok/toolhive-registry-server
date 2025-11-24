package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

const (
	openapiPath = "/openapi.yaml"
)

func TestAPISources(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "API Registry Handler Suite")
}

var _ = Describe("APIRegistryHandler", func() {
	var (
		handler    sources.RegistryHandler
		ctx        context.Context
		mockServer *httptest.Server
	)

	BeforeEach(func() {
		handler = sources.NewAPIRegistryHandler()
		ctx = context.Background()
	})

	AfterEach(func() {
		if mockServer != nil {
			mockServer.Close()
		}
	})

	Describe("Upstream Format Validation", func() {
		Context("Upstream Format", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case openapiPath:
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
info:
  title: Official MCP Registry
  description: |
    A community driven registry service for Model Context Protocol (MCP) servers.

    [GitHub repository](https://github.com/modelcontextprotocol/registry)
  version: 1.0.0
openapi: 3.1.0
`))
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
			})

			It("should detect upstream format", func() {
				registryConfig := &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
				// Should detect as upstream but fail fetch (Phase 2 not implemented)
				_, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("upstream MCP Registry API support not yet implemented"))
			})
		})

		Context("Invalid Format", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					// Return 404 for all endpoints
					w.WriteHeader(http.StatusNotFound)
				}))
			})

			It("should fail when invalid format specified", func() {
				registryConfig := &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatToolHive,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}

				_, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("unsupported format"))
			})
		})
	})
})
