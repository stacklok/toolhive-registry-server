// Package main implements thv-auth-test, a CLI tool for testing OAuth authentication
// flow against the ToolHive registry server. It performs a complete OAuth discovery
// and client credentials flow to validate the authentication setup.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var (
	registryURL  string
	clientID     string
	clientSecret string
	scope        string
	verbose      bool
)

type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

type authServerMetadata struct {
	TokenEndpoint string `json:"token_endpoint"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "thv-auth-test",
		Short: "Test OAuth authentication against the ToolHive registry server",
		Long: `A CLI tool for testing OAuth authentication flow against the registry server.
It performs a complete OAuth discovery and client credentials flow.`,
		RunE: runAuthTest,
	}

	rootCmd.Flags().StringVar(&registryURL, "registry-url", "", "Registry server URL (env: REGISTRY_URL)")
	rootCmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client ID (env: OAUTH_CLIENT_ID)")
	rootCmd.Flags().StringVar(&clientSecret, "client-secret", "", "OAuth client secret (env: OAUTH_CLIENT_SECRET)")
	rootCmd.Flags().StringVar(&scope, "scope", "mcp-registry:read mcp-registry:write", "OAuth scope")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show step-by-step output")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runAuthTest(_ *cobra.Command, _ []string) error {
	// Initialize configuration
	if err := initializeConfig(); err != nil {
		return err
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	serversURL := strings.TrimSuffix(registryURL, "/") + "/registry/v0.1/servers"

	// Step 1: Test unauthenticated request and get WWW-Authenticate header
	wwwAuth, err := testUnauthenticatedRequest(client, serversURL)
	if err != nil {
		return err
	}

	// Step 2: Parse WWW-Authenticate header
	logStep(2, "Parsing WWW-Authenticate header...")
	resourceMetadataURL, err := parseWWWAuthenticate(wwwAuth)
	if err != nil {
		return fmt.Errorf("failed to parse WWW-Authenticate header: %w", err)
	}
	logVerbose("  resource_metadata: %s", resourceMetadataURL)

	// Step 3-5: Perform OAuth discovery and get token
	token, err := performOAuthDiscovery(client, resourceMetadataURL)
	if err != nil {
		return err
	}

	// Step 6: Retry with authentication
	if err := testAuthenticatedRequest(client, serversURL, token.AccessToken); err != nil {
		return err
	}

	fmt.Println("\nSuccess! Full OAuth discovery flow validated.")
	return nil
}

// initializeConfig loads configuration from flags and environment variables
func initializeConfig() error {
	// Load from env vars if not set via flags
	if registryURL == "" {
		registryURL = os.Getenv("REGISTRY_URL")
	}
	if clientID == "" {
		clientID = os.Getenv("OAUTH_CLIENT_ID")
	}
	if clientSecret == "" {
		clientSecret = os.Getenv("OAUTH_CLIENT_SECRET")
	}

	// Validate required parameters
	if registryURL == "" {
		return fmt.Errorf("registry-url is required (set via --registry-url or REGISTRY_URL)")
	}
	if clientID == "" {
		return fmt.Errorf("client-id is required (set via --client-id or OAUTH_CLIENT_ID)")
	}
	if clientSecret == "" {
		return fmt.Errorf("client-secret is required (set via --client-secret or OAUTH_CLIENT_SECRET)")
	}

	return nil
}

// testUnauthenticatedRequest makes an unauthenticated request and returns the WWW-Authenticate header
func testUnauthenticatedRequest(client *http.Client, serversURL string) (string, error) {
	logStep(1, "Testing unauthenticated request...")

	resp, err := client.Get(serversURL)
	if err != nil {
		return "", fmt.Errorf("failed to make unauthenticated request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		return "", fmt.Errorf("expected 401 Unauthorized, got %d %s", resp.StatusCode, resp.Status)
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	if wwwAuth == "" {
		return "", fmt.Errorf("missing WWW-Authenticate header in 401 response")
	}

	logVerbose("  GET /registry/v0.1/servers -> %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	logVerbose("  WWW-Authenticate: %s", wwwAuth)

	return wwwAuth, nil
}

// performOAuthDiscovery performs the OAuth discovery flow and returns an access token
func performOAuthDiscovery(client *http.Client, resourceMetadataURL string) (*tokenResponse, error) {
	// Step 3: Fetch protected resource metadata
	logStep(3, "Fetching protected resource metadata...")
	prm, err := fetchProtectedResourceMetadata(client, resourceMetadataURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch protected resource metadata: %w", err)
	}

	if len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("no authorization servers found in protected resource metadata")
	}

	authServerURL := prm.AuthorizationServers[0]
	logVerbose("  authorization_servers: %v", prm.AuthorizationServers)

	// Step 4: Fetch authorization server metadata
	logStep(4, "Fetching authorization server metadata...")
	asm, err := fetchAuthServerMetadata(client, authServerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch authorization server metadata: %w", err)
	}
	logVerbose("  token_endpoint: %s", asm.TokenEndpoint)

	// Step 5: Acquire access token
	logStep(5, "Acquiring access token...")
	token, err := acquireToken(client, asm.TokenEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire access token: %w", err)
	}
	logVerbose("  Token acquired (expires in %ds)", token.ExpiresIn)

	return token, nil
}

// testAuthenticatedRequest makes an authenticated request to verify the token works
func testAuthenticatedRequest(client *http.Client, serversURL, accessToken string) error {
	logStep(6, "Retrying with authentication...")

	req, err := http.NewRequest(http.MethodGet, serversURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create authenticated request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", accessToken))

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make authenticated request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("expected 200 OK, got %d %s: %s", resp.StatusCode, resp.Status, string(body))
	}

	logVerbose("  GET /registry/v0.1/servers -> %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	return nil
}

func parseWWWAuthenticate(header string) (string, error) {
	// Parse resource_metadata from WWW-Authenticate header
	// Format: Bearer realm="...", error="...", resource_metadata="..."
	re := regexp.MustCompile(`resource_metadata="([^"]+)"`)
	matches := re.FindStringSubmatch(header)
	if len(matches) < 2 {
		return "", fmt.Errorf("resource_metadata not found in WWW-Authenticate header")
	}
	return matches[1], nil
}

func fetchProtectedResourceMetadata(client *http.Client, metadataURL string) (*protectedResourceMetadata, error) {
	resp, err := client.Get(metadataURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var prm protectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&prm); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &prm, nil
}

func fetchAuthServerMetadata(client *http.Client, authServerURL string) (*authServerMetadata, error) {
	// Try .well-known/oauth-authorization-server first, then .well-known/openid-configuration
	baseURL := strings.TrimSuffix(authServerURL, "/")

	endpoints := []string{
		baseURL + "/.well-known/oauth-authorization-server",
		baseURL + "/.well-known/openid-configuration",
	}

	var lastErr error
	for _, endpoint := range endpoints {
		resp, err := client.Get(endpoint)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			continue
		}

		var asm authServerMetadata
		if err := json.NewDecoder(resp.Body).Decode(&asm); err != nil {
			lastErr = fmt.Errorf("failed to decode response: %w", err)
			continue
		}

		if asm.TokenEndpoint == "" {
			lastErr = fmt.Errorf("token_endpoint not found in metadata")
			continue
		}

		return &asm, nil
	}

	return nil, fmt.Errorf("failed to fetch auth server metadata from any endpoint: %w", lastErr)
}

func acquireToken(client *http.Client, tokenEndpoint string) (*tokenResponse, error) {
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)
	if scope != "" {
		data.Set("scope", scope)
	}

	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var token tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &token, nil
}

func logStep(step int, message string) {
	if verbose {
		fmt.Printf("Step %d: %s\n", step, message)
	}
}

func logVerbose(format string, args ...interface{}) {
	if verbose {
		fmt.Printf(format+"\n", args...)
	}
}
