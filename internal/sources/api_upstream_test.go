package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
)

const upstreamOpenapiPath = "/openapi.yaml"

var _ = Describe("UpstreamAPIHandler", func() {
	var (
		handler    *sources.UpstreamAPIHandler
		ctx        context.Context
		mockServer *httptest.Server
	)

	BeforeEach(func() {
		httpClient := httpclient.NewDefaultClient(0)
		handler = sources.NewUpstreamAPIHandler(httpClient)
		ctx = context.Background()
	})

	AfterEach(func() {
		if mockServer != nil {
			mockServer.Close()
		}
	})

	Describe("Validate", func() {
		Context("Valid Upstream MCP Registry API", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Official MCP Registry
  description: |
    A community driven registry service for Model Context Protocol (MCP) servers.

    [GitHub repository](https://github.com/modelcontextprotocol/registry) | [Documentation](https://github.com/modelcontextprotocol/registry/tree/main/docs)
  version: 1.0.0
paths:
  /v0/servers:
    get:
      summary: List servers
`))
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}))
			})

			It("should validate successfully", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("Missing /openapi.yaml endpoint", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusNotFound)
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to fetch /openapi.yaml"))
			})
		})

		Context("Invalid YAML", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{invalid: yaml: [unclosed`))
					}
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse /openapi.yaml"))
			})
		})

		Context("Missing info section", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
paths:
  /v0/servers:
    get:
      summary: List servers
`))
					}
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("missing 'info' section"))
			})
		})

		Context("Missing version field", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: A registry without version
`))
					}
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("missing 'version' field"))
			})
		})

		Context("Wrong version", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: Contains GitHub URL https://github.com/modelcontextprotocol/registry
  version: 2.0.0
`))
					}
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("version is 2.0.0, expected 1.0.0"))
			})
		})

		Context("Missing description field", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  version: 1.0.0
`))
					}
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("missing 'description' field"))
			})
		})

		Context("Description without GitHub URL", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: A registry without the expected GitHub URL
  version: 1.0.0
`))
					}
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("does not contain expected GitHub URL"))
			})
		})

		Context("Version as number instead of string", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == upstreamOpenapiPath {
						w.Header().Set("Content-Type", "application/x-yaml")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`
openapi: 3.1.0
info:
  title: Some Registry
  description: Contains https://github.com/modelcontextprotocol/registry
  version: 1.0
`))
					}
				}))
			})

			It("should fail validation", func() {
				err := handler.Validate(ctx, mockServer.URL)
				Expect(err).To(HaveOccurred())
				// YAML will parse 1.0 as float, not string
				Expect(err.Error()).To(ContainSubstring("missing 'version' field"))
			})
		})
	})

	Describe("FetchRegistry", func() {
		var registryConfig *config.Config

		BeforeEach(func() {
			registryConfig = &config.Config{
				Source: config.SourceConfig{
					Type: config.SourceTypeAPI,
					API:  &config.APIConfig{},
				},
			}
		})

		Context("Phase 2 not implemented", func() {
			It("should return not implemented error", func() {
				_, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not yet implemented"))
				Expect(err.Error()).To(ContainSubstring("Phase 2"))
			})
		})
	})

	Describe("CurrentHash", func() {
		var registryConfig *config.Config

		BeforeEach(func() {
			registryConfig = &config.Config{
				Source: config.SourceConfig{
					Type: config.SourceTypeAPI,
					API:  &config.APIConfig{},
				},
			}
		})

		Context("Phase 2 not implemented", func() {
			It("should return not implemented error", func() {
				_, err := handler.CurrentHash(ctx, registryConfig)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not yet implemented"))
				Expect(err.Error()).To(ContainSubstring("Phase 2"))
			})
		})
	})
})
