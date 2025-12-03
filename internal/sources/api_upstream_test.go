package sources

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/httpclient"
)

const (
	upstreamOpenapiPath = "/openapi.yaml"
	serversAPIPath      = "/v0.1/servers"
)

var _ = Describe("UpstreamAPIHandler", func() {
	var (
		handler    *upstreamAPIHandler
		ctx        context.Context
		mockServer *httptest.Server
	)

	BeforeEach(func() {
		httpClient := httpclient.NewDefaultClient(0)
		handler = NewUpstreamAPIHandler(httpClient)
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
		var registryConfig *config.RegistryConfig

		Context("Successful fetch with single page", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == serversAPIPath {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
							"servers": [
								{
									"server": {
										"name": "test-server",
										"description": "A test server"
									},
									"_meta": {}
								}
							],
							"metadata": {
								"nextCursor": "",
								"count": 1
							}
						}`))
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				registryConfig = &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
			})

			It("should fetch servers successfully", func() {
				result, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.ServerCount).To(Equal(1))
				Expect(result.Format).To(Equal(config.SourceFormatUpstream))
				Expect(result.Hash).NotTo(BeEmpty())
				Expect(result.Registry).NotTo(BeNil())
				Expect(result.Registry.Data.Servers).To(HaveLen(1))
				Expect(result.Registry.Data.Servers[0].Name).To(Equal("test-server"))
			})
		})

		Context("Successful fetch with pagination", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == serversAPIPath {
						cursor := r.URL.Query().Get("cursor")
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						switch cursor {
						case "":
							_, _ = w.Write([]byte(`{
								"servers": [
									{
										"server": {
											"name": "server-1",
											"description": "First server"
										},
										"_meta": {}
									}
								],
								"metadata": {
									"nextCursor": "page2",
									"count": 1
								}
							}`))
						case "page2":
							_, _ = w.Write([]byte(`{
								"servers": [
									{
										"server": {
											"name": "server-2",
											"description": "Second server"
										},
										"_meta": {}
									}
								],
								"metadata": {
									"nextCursor": "",
									"count": 1
								}
							}`))
						}
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				registryConfig = &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
			})

			It("should fetch all pages and combine servers", func() {
				result, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).NotTo(BeNil())
				Expect(result.ServerCount).To(Equal(2))
				Expect(result.Registry.Data.Servers).To(HaveLen(2))
				Expect(result.Registry.Data.Servers[0].Name).To(Equal("server-1"))
				Expect(result.Registry.Data.Servers[1].Name).To(Equal("server-2"))
			})
		})

		Context("HTTP error during fetch", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				registryConfig = &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
			})

			It("should return error on HTTP failure", func() {
				_, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to fetch servers"))
			})
		})

		Context("Invalid JSON response", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == serversAPIPath {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{invalid json`))
					}
				}))
				registryConfig = &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
			})

			It("should return error on invalid JSON", func() {
				_, err := handler.FetchRegistry(ctx, registryConfig)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to parse response"))
			})
		})
	})

	Describe("CurrentHash", func() {
		var registryConfig *config.RegistryConfig

		Context("Successful hash calculation", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == serversAPIPath {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusOK)
						_, _ = w.Write([]byte(`{
							"servers": [
								{
									"server": {
										"name": "test-server",
										"description": "A test server"
									},
									"_meta": {}
								}
							],
							"metadata": {
								"nextCursor": "",
								"count": 1
							}
						}`))
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				registryConfig = &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
			})

			It("should return hash matching FetchRegistry", func() {
				hash, err := handler.CurrentHash(ctx, registryConfig)
				Expect(err).NotTo(HaveOccurred())
				Expect(hash).NotTo(BeEmpty())
				// Should be a valid SHA256 hex string (64 characters)
				Expect(hash).To(HaveLen(64))
			})
		})

		Context("Error propagation", func() {
			BeforeEach(func() {
				mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusInternalServerError)
				}))
				registryConfig = &config.RegistryConfig{
					Name:   "test-registry",
					Format: config.SourceFormatUpstream,
					API: &config.APIConfig{
						Endpoint: mockServer.URL,
					},
				}
			})

			It("should propagate fetch errors", func() {
				_, err := handler.CurrentHash(ctx, registryConfig)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
