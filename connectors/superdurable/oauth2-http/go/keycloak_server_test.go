//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	keycloakRealm        = "dex-oauth2-http"
	keycloakClientID     = "dex-oauth2-http-test"
	keycloakRedirectURI  = "http://dex-test.invalid/callback"
	keycloakUsername     = "alice"
	defaultKeycloakImage = "quay.io/keycloak/keycloak:26.8.0"
)

// keycloakServer is one Keycloak container that imports testdata/keycloak/realm.json: 60-second access
// tokens, refresh tokens that stop working once they are used, and a confidential client that requires PKCE.
type keycloakServer struct {
	baseURL       string
	containerName string
	clientSecret  string
	userPassword  string
	endpoints     keycloakEndpoints
}

type keycloakEndpoints struct {
	Authorization string `json:"authorization_endpoint"`
	Token         string `json:"token_endpoint"`
	UserInfo      string `json:"userinfo_endpoint"`
}

var (
	startKeycloakOnce sync.Once
	sharedKeycloak    *keycloakServer
	keycloakStartErr  error
)

// TestMain removes the Keycloak container that the first test started, if any.
func TestMain(m *testing.M) {
	exitCode := m.Run()
	if sharedKeycloak != nil {
		_ = exec.Command("docker", "rm", "--force", sharedKeycloak.containerName).Run()
	}
	os.Exit(exitCode)
}

// requireKeycloak starts the shared Keycloak container on first use and fails the test when Docker or the
// image is unavailable; a missing service is never a skipped test.
func requireKeycloak(t *testing.T) *keycloakServer {
	t.Helper()
	startKeycloakOnce.Do(func() { sharedKeycloak, keycloakStartErr = startKeycloak() })
	require.NoError(t, keycloakStartErr)
	return sharedKeycloak
}

func startKeycloak() (*keycloakServer, error) {
	image := os.Getenv("KEYCLOAK_IMAGE")
	if image == "" {
		image = defaultKeycloakImage
	}
	server := &keycloakServer{
		containerName: "dex-oauth2-http-keycloak-" + randomHex(4), clientSecret: randomHex(16), userPassword: randomHex(16),
	}
	steps := [][]string{
		{"create", "--name", server.containerName, "--publish", "127.0.0.1::8080",
			"--env", "KC_BOOTSTRAP_ADMIN_USERNAME=admin", "--env", "KC_BOOTSTRAP_ADMIN_PASSWORD=" + randomHex(16),
			"--env", "DEX_TEST_CLIENT_SECRET=" + server.clientSecret, "--env", "DEX_TEST_USER_PASSWORD=" + server.userPassword,
			image, "start-dev", "--import-realm"},
		// The image has no import directory, so copying the realm directory creates it.
		{"cp", "testdata/keycloak", server.containerName + ":/opt/keycloak/data/import"},
		{"start", server.containerName},
	}
	for _, arguments := range steps {
		if output, err := exec.Command("docker", arguments...).CombinedOutput(); err != nil {
			_ = exec.Command("docker", "rm", "--force", server.containerName).Run()
			// The arguments carry the test-only secrets, so the failure names the step, not the command line.
			return nil, fmt.Errorf("docker %s failed: %w: %s", arguments[0], err, strings.TrimSpace(string(output)))
		}
	}
	port, err := exec.Command("docker", "port", server.containerName, "8080/tcp").Output()
	if err != nil {
		return server, fmt.Errorf("docker port: %w", err)
	}
	server.baseURL = "http://" + strings.TrimSpace(strings.Split(string(port), "\n")[0])
	return server, server.waitUntilReady()
}

// waitUntilReady polls the realm's discovery document until Keycloak has imported the realm.
func (server *keycloakServer) waitUntilReady() error {
	discoveryURL := server.baseURL + "/realms/" + keycloakRealm + "/.well-known/openid-configuration"
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		response, err := http.Get(discoveryURL)
		if err == nil {
			contents, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return json.Unmarshal(contents, &server.endpoints)
			}
		}
		time.Sleep(2 * time.Second)
	}
	logs, _ := exec.Command("docker", "logs", "--tail", "40", server.containerName).CombinedOutput()
	return fmt.Errorf("keycloak did not serve realm %s within four minutes; last log lines:\n%s", keycloakRealm, logs)
}

// userInfoPath is the UserInfo endpoint's path, which the operation appends to apiBaseUrl.
func (server *keycloakServer) userInfoPath(t *testing.T) string {
	t.Helper()
	require.True(t, strings.HasPrefix(server.endpoints.UserInfo, server.baseURL+"/"), "discovery reports %s", server.endpoints.UserInfo)
	return strings.TrimPrefix(server.endpoints.UserInfo, server.baseURL)
}

// keycloakGrant is the result of one token endpoint answer.
type keycloakGrant struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
}

// authorize signs alice in without a browser, as Dex Web's callback would after the user approves: it opens
// the authorization endpoint with an S256 PKCE challenge, submits the login form, and reads the code from the
// redirect it does not follow. It returns the code with the PKCE verifier that redeems it.
func (server *keycloakServer) authorize(t *testing.T) (code string, verifier string) {
	t.Helper()
	verifier = randomBase64URL(48)
	challenge := sha256.Sum256([]byte(verifier))
	state := randomHex(8)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	browser := &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	authorizationURL := server.endpoints.Authorization + "?" + url.Values{
		"response_type": {"code"}, "client_id": {keycloakClientID}, "redirect_uri": {keycloakRedirectURI},
		"scope": {"openid profile"}, "state": {state},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"},
	}.Encode()
	loginPage := readBody(t, mustGet(t, browser, authorizationURL), http.StatusOK)
	action := regexp.MustCompile(`<form[^>]*id="kc-form-login"[^>]*action="([^"]+)"`).FindStringSubmatch(loginPage)
	require.Len(t, action, 2, "the Keycloak login page has no login form")

	response, err := browser.PostForm(html.UnescapeString(action[1]), url.Values{"username": {keycloakUsername}, "password": {server.userPassword}})
	require.NoError(t, err)
	_ = response.Body.Close()
	require.Equal(t, http.StatusFound, response.StatusCode, "the login redirects to the client")
	location, err := url.Parse(response.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, keycloakRedirectURI, location.Scheme+"://"+location.Host+location.Path)
	require.Equal(t, state, location.Query().Get("state"))
	code = location.Query().Get("code")
	require.NotEmpty(t, code, "the redirect carries an authorization code")
	return code, verifier
}

// exchangeCode redeems an authorization code at the token endpoint with the client secret in the form body.
func (server *keycloakServer) exchangeCode(t *testing.T, code string, verifier string) (keycloakGrant, int) {
	t.Helper()
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {keycloakRedirectURI},
		"client_id": {keycloakClientID}, "client_secret": {server.clientSecret},
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	response, err := (&http.Client{Timeout: 30 * time.Second}).PostForm(server.endpoints.Token, form)
	require.NoError(t, err)
	defer response.Body.Close()
	var grant keycloakGrant
	require.NoError(t, json.NewDecoder(response.Body).Decode(&grant))
	return grant, response.StatusCode
}

// signIn runs the whole authorization code flow with PKCE and returns the first grant.
func (server *keycloakServer) signIn(t *testing.T) keycloakGrant {
	t.Helper()
	code, verifier := server.authorize(t)
	grant, status := server.exchangeCode(t, code, verifier)
	require.Equal(t, http.StatusOK, status, "token endpoint error %q", grant.Error)
	require.NotEmpty(t, grant.AccessToken)
	require.NotEmpty(t, grant.RefreshToken)
	return grant
}

// userInfoStatus calls the UserInfo endpoint directly with accessToken.
func (server *keycloakServer) userInfoStatus(t *testing.T, accessToken string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.endpoints.UserInfo, nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := (&http.Client{Timeout: 30 * time.Second}).Do(request)
	require.NoError(t, err)
	_ = response.Body.Close()
	return response.StatusCode
}

func mustGet(t *testing.T, client *http.Client, address string) *http.Response {
	t.Helper()
	response, err := client.Get(address)
	require.NoError(t, err)
	return response
}

func readBody(t *testing.T, response *http.Response, expectedStatus int) string {
	t.Helper()
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, expectedStatus, response.StatusCode)
	return string(contents)
}

func randomHex(byteCount int) string {
	contents := make([]byte, byteCount)
	if _, err := rand.Read(contents); err != nil {
		panic(err)
	}
	return hex.EncodeToString(contents)
}

func randomBase64URL(byteCount int) string {
	contents := make([]byte, byteCount)
	if _, err := rand.Read(contents); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(contents)
}
