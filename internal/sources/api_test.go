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
					case "/v0.1/servers":
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
							"servers": [
								{
									"server": {
										"name": "test-server",
										"description": "A test MCP server"
									},
									"_meta": {}
								}
							],
							"metadata": {
								"nextCursor": "",
								"count": 1
							}
						}`))
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
			})

			It("should detect upstream format and fetch successfully", func() {
				registryConfig := &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
				result, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.ServerCount).To(Equal(1))
				Expect(result.Format).To(Equal(config.SourceFormatUpstream))
				Expect(result.Registry.Data.Servers[0].Name).To(Equal("test-server"))
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
