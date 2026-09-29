// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// GoogleOAuthAuthMethodID identifies interactive Google OAuth authorization.
	GoogleOAuthAuthMethodID = "google-oauth"
	// WorkspaceDomainDelegationAuthMethodID identifies administrator-approved service-account delegation.
	WorkspaceDomainDelegationAuthMethodID = "workspace-domain-delegation"

	googleOAuthTokenEndpoint = "https://oauth2.googleapis.com/token"
	credentialRefreshSkew    = 5 * time.Minute
)

var gmailOAuthScopes = []string{
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send",
}

// CredentialRefreshDriver refreshes Google OAuth and Workspace delegated credentials.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	ErrorCode    string `json:"error"`
}

type serviceAccountKey struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 25-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access credential is absent or expires within five minutes.
func (driver *CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AccessToken.Reveal() == "" || state.ExpiresAt == nil {
		return true
	}
	return !state.ExpiresAt.After(state.Now.Add(credentialRefreshSkew))
}

// Refresh obtains a replacement access credential for the selected authorization method.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Gmail credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID:
		return driver.refreshGoogleOAuth(ctx, state.Credentials)
	case WorkspaceDomainDelegationAuthMethodID:
		return driver.refreshWorkspaceDelegation(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Gmail authorization method is not supported")
	}
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped credential returned by the hosted broker.
// Renewal material is intentionally absent from this short-lived representation.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		AccessToken  string `json:"access_token"`
		PrimaryEmail string `json:"primary_email"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Gmail resolved credential is invalid")
	}
	credentials := Credentials{
		AuthMethodID: fields.AuthMethodID,
		AccessToken:  sdkgo.NewSecretString(fields.AccessToken),
		PrimaryEmail: fields.PrimaryEmail,
	}
	return credentials, validateResolvedCredentials(credentials)
}

// EncodeCredentialsJSON encodes complete credential material for trusted atomic persistence.
func EncodeCredentialsJSON(credentials Credentials) ([]byte, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return encodeLocalCredentials(credentials)
}

func (driver *CredentialRefreshDriver) refreshGoogleOAuth(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if credentials.OAuthClientID == "" || credentials.OAuthClientSecret.Reveal() == "" || credentials.RefreshToken.Reveal() == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Google OAuth refresh material is incomplete"))
	}
	form := url.Values{
		"client_id":     {credentials.OAuthClientID},
		"client_secret": {credentials.OAuthClientSecret.Reveal()},
		"grant_type":    {"refresh_token"},
		"refresh_token": {credentials.RefreshToken.Reveal()},
	}
	token, err := driver.exchangeToken(ctx, googleOAuthTokenEndpoint, form)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if err := validateReturnedScopes(token.Scope); err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	credentials.AccessToken = sdkgo.NewSecretString(token.AccessToken)
	if token.RefreshToken != "" {
		credentials.RefreshToken = sdkgo.NewSecretString(token.RefreshToken)
	}
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second),
	}, nil
}

func (driver *CredentialRefreshDriver) refreshWorkspaceDelegation(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	key, privateKey, err := parseServiceAccountKey(credentials.ServiceAccountKey.Reveal())
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	delegatedUser := strings.TrimSpace(credentials.DelegatedUser)
	if delegatedUser == "" || !strings.Contains(delegatedUser, "@") {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Workspace delegated user is invalid"))
	}
	now := driver.now().UTC()
	assertion, err := signServiceAccountAssertion(key, privateKey, delegatedUser, now)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	token, err := driver.exchangeToken(ctx, key.TokenURI, url.Values{
		"assertion":  {assertion},
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
	})
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if err := validateReturnedScopes(token.Scope); err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	credentials.AccessToken = sdkgo.NewSecretString(token.AccessToken)
	credentials.PrimaryEmail = delegatedUser
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   now.Add(time.Duration(token.ExpiresIn) * time.Second),
	}, nil
}

func (driver *CredentialRefreshDriver) exchangeToken(
	ctx context.Context,
	tokenEndpoint string,
	form url.Values,
) (googleTokenResponse, error) {
	endpoint, err := url.Parse(tokenEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return googleTokenResponse{}, errors.New("Google token endpoint is invalid")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), strings.NewReader(form.Encode()))
	if err != nil {
		return googleTokenResponse{}, errors.New("Google token request could not be built")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := driver.httpClient.Do(request)
	if err != nil {
		return googleTokenResponse{}, fmt.Errorf("Google token endpoint is unavailable: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return googleTokenResponse{}, errors.New("Google token response could not be read")
	}
	var token googleTokenResponse
	if err := json.Unmarshal(contents, &token); err != nil {
		return googleTokenResponse{}, errors.New("Google token response is invalid")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if token.ErrorCode == "invalid_grant" || token.ErrorCode == "invalid_client" || token.ErrorCode == "unauthorized_client" {
			return googleTokenResponse{}, sdkgo.NewReauthorizationRequiredError(fmt.Errorf("Google rejected credential refresh with %s", token.ErrorCode))
		}
		return googleTokenResponse{}, fmt.Errorf("Google token endpoint returned HTTP %d", response.StatusCode)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 || !strings.EqualFold(token.TokenType, "Bearer") {
		return googleTokenResponse{}, errors.New("Google token response omitted required fields")
	}
	return token, nil
}

func parseServiceAccountKey(raw string) (serviceAccountKey, *rsa.PrivateKey, error) {
	var key serviceAccountKey
	if err := json.Unmarshal([]byte(raw), &key); err != nil {
		return serviceAccountKey{}, nil, errors.New("Workspace service-account key is invalid JSON")
	}
	if key.ClientEmail == "" || key.PrivateKey == "" || key.TokenURI == "" {
		return serviceAccountKey{}, nil, errors.New("Workspace service-account key is incomplete")
	}
	endpoint, err := url.Parse(key.TokenURI)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return serviceAccountKey{}, nil, errors.New("Workspace service-account token endpoint is invalid")
	}
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		return serviceAccountKey{}, nil, errors.New("Workspace service-account private key is invalid")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		privateKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return serviceAccountKey{}, nil, errors.New("Workspace service-account private key must use RSA")
		}
		return key, privateKey, nil
	}
	privateKey, pkcs1Err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if pkcs1Err != nil {
		return serviceAccountKey{}, nil, errors.New("Workspace service-account private key is invalid")
	}
	return key, privateKey, nil
}

func signServiceAccountAssertion(
	key serviceAccountKey,
	privateKey *rsa.PrivateKey,
	delegatedUser string,
	now time.Time,
) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"aud":   key.TokenURI,
		"exp":   now.Add(time.Hour).Unix(),
		"iat":   now.Unix(),
		"iss":   key.ClientEmail,
		"scope": strings.Join(gmailOAuthScopes, " "),
		"sub":   delegatedUser,
	})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("Workspace service-account assertion could not be signed")
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func validateReturnedScopes(scope string) error {
	if strings.TrimSpace(scope) == "" {
		return nil
	}
	granted := make(map[string]bool)
	for _, value := range strings.Fields(scope) {
		granted[value] = true
	}
	missing := make([]string, 0, len(gmailOAuthScopes))
	for _, required := range gmailOAuthScopes {
		if !granted[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return fmt.Errorf("Google credential lacks required Gmail scopes: %s", strings.Join(missing, ", "))
	}
	return nil
}
