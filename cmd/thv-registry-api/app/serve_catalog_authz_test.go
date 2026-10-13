package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/stacklok/toolhive-registry-server/database"
)

//nolint:paralleltest // Real selected CLI, Viper flags and OAuth over disposable PostgreSQL.
func TestDefaultServeOAuthCatalogAndRetiredRoutes(t *testing.T) {
	t.Setenv("THV_REGISTRY_INSECURE_URL", "true") // Disposable loopback OAuth fixture only.
	previousConfig, previousAddress, previousInternal := viper.Get("config"), viper.Get("address"), viper.Get("internal-address")
	t.Cleanup(func() {
		viper.Set("config", previousConfig)
		viper.Set("address", previousAddress)
		viper.Set("internal-address", previousInternal)
	})
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	var issuerURL string
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuerURL, "jwks_uri": issuerURL + "/jwks"})
			return
		}
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{
			"kty": "RSA", "kid": "test", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}}))
	}))
	issuerURL = issuer.URL
	defer issuer.Close()
	token := func(subject string) string {
		t.Helper()
		value := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
			"iss": issuer.URL, "sub": subject, "aud": "registry-cli", "exp": time.Now().Add(time.Hour).Unix(),
			"groups": []string{subject}, "role": "not-a-legacy-role",
		})
		value.Header["kid"] = "test"
		encoded, err := value.SignedString(key)
		require.NoError(t, err)
		return encoded
	}
	conn, _ := database.SetupTestDB(t)
	configPath := writeServeCatalogConfig(t, conn.Config().Host, int(conn.Config().Port), conn.Config().User, conn.Config().Database, conn.Config().Password)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	text := strings.Replace(string(data), "auth:\n mode: oauth", "auth:\n mode: oauth\n oauth:\n  providers:\n   - name: test\n     issuerUrl: "+issuer.URL+"\n     audience: registry-cli\n     jwksUrl: "+issuer.URL+"/jwks\n     allowPrivateIP: true", 1)
	text = strings.Replace(text, "enabled: false", "enabled: true", 1)
	text = strings.Replace(text, "allowAnonymousCatalog: true", "allowAnonymousCatalog: false", 1)
	require.NoError(t, os.WriteFile(configPath, []byte(text), 0600))
	policy := filepath.Join(filepath.Dir(configPath), "policy.cedar")
	require.NoError(t, os.WriteFile(policy, []byte(`permit(principal,action,resource) when { principal.authenticated };`), 0600))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	viper.Set("config", configPath)
	viper.Set("address", address)
	viper.Set("internal-address", "127.0.0.1:0")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	command := &cobra.Command{}
	command.SetContext(ctx)
	command.Flags().String("auth-mode", "", "")
	command.Flags().Bool("initialize-empty", true, "")
	finished := make(chan error, 1)
	go func() { finished <- runServe(command, nil) }()
	client := &http.Client{Timeout: 5 * time.Second}
	require.Eventually(t, func() bool {
		response, err := client.Get("http://" + address + "/v1/sources")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		return response.StatusCode == http.StatusUnauthorized
	}, 10*time.Second, 20*time.Millisecond)
	request := func(method, path, body, credential string, status int) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, "http://"+address+path, strings.NewReader(body))
		require.NoError(t, err)
		if credential != "" {
			req.Header.Set("Authorization", "Bearer "+credential)
		}
		response, err := client.Do(req)
		require.NoError(t, err)
		defer response.Body.Close()
		payload, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Equal(t, status, response.StatusCode, "%s %s: %s", method, path, payload)
		if status == http.StatusUnauthorized {
			require.Contains(t, response.Header.Get("WWW-Authenticate"), "Bearer")
		}
		return payload
	}
	first, second := token("finance"), token("engineering")
	request("GET", "/registry/public/v0.1/servers", "", "", 401)
	request("GET", "/v1/sources", "", "", 401)
	request("GET", "/openapi.json", "", "", 401)
	metadata := request("GET", "/.well-known/oauth-protected-resource", "", "", 200)
	require.NotContains(t, string(metadata), "scopes_supported")
	openAPI := request("GET", "/openapi.json", "", first, 200)
	require.NotContains(t, string(openAPI), "superAdmin")
	require.Contains(t, string(openAPI), "httpapi.publishRequest")
	request("POST", "/v1/entries", `{"server":{"$schema":"https://example.org/server.schema.json","name":"com.example/shared","description":"shared catalog","version":"1"}}`, first, 201)
	page := request("GET", "/registry/public/v0.1/servers?limit=1", "", first, 200)
	require.Equal(t, page, request("GET", "/registry/public/v0.1/servers?limit=1", "", second, 200))
	require.Contains(t, string(page), "shared catalog")
	request("GET", "/registry/public/v0.1/servers/com.example%2Fshared/versions", "", first, 200)
	request("GET", "/registry/public/v0.1/servers/com.example%2Fshared/versions/latest", "", first, 200)
	for _, family := range []string{"skills", "plugins"} {
		prefix := "/registry/public/v0.1/x/dev.toolhive/" + family
		request("GET", prefix, "", first, 200)
		request("GET", prefix+"/", "", first, 200)
		request("GET", prefix+"/com.example/missing", "", first, 404)
		request("GET", prefix+"/com.example/missing/versions", "", first, 200)
		request("GET", prefix+"/com.example/missing/versions/latest", "", first, 404)
	}
	for _, path := range []string{"/v1/sources", "/v1/sources/managed", "/v1/sources/managed/entries", "/v1/registries", "/v1/registries/public", "/v1/registries/public/entries"} {
		payload := request("GET", path, "", first, 200)
		require.NotContains(t, string(payload), `"claims"`)
	}
	inline, err := json.Marshal(map[string]any{"file": map[string]string{"data": `{"version":"1.0.0","meta":{"last_updated":"2025-01-15T10:30:00Z"},"data":{"servers":[{"$schema":"https://example.org/server.schema.json","name":"com.example/inline","description":"inline catalog","version":"1"}]}}`}})
	require.NoError(t, err)
	request("PUT", "/v1/sources/api-owned", string(inline), first, 201)
	request("PUT", "/v1/registries/api-owned", `{"sources":["api-owned"]}`, first, 201)
	request("DELETE", "/v1/registries/api-owned", "", first, 204)
	request("DELETE", "/v1/sources/api-owned", "", first, 204)
	request("PUT", "/v1/registries/rejected", `{"sources":["managed"],"claims":null}`, first, 400)
	request("GET", "/v1/me", "", first, 410)
	request("GET", "/v1/entries/server/com.example%2Fshared/claims", "", first, 410)
	request("PUT", "/v1/entries/server/com.example%2Fshared/claims", `{"claims":{}}`, first, 410)
	request("DELETE", "/v1/entries/server/com.example%2Fshared/versions/1", "", first, 204)
	cancel()
	require.NoError(t, <-finished)
}
