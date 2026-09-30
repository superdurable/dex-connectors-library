// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauthtoken

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const testTokenEndpointURL = "https://auth.example.test/oauth/token"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func tokenHTTPResponse(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	contents, err := json.Marshal(body)
	require.NoError(t, err)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(contents)))}
}

func testTokenEndpoint(transport roundTripFunc) TokenEndpoint {
	return TokenEndpoint{
		ProviderName:       "Example",
		URL:                testTokenEndpointURL,
		HTTPClient:         &http.Client{Transport: transport},
		TerminalErrorCodes: []string{"invalid_grant", "bad_refresh_token"},
	}
}

func successfulTokenBody() map[string]any {
	return map[string]any{"access_token": "new-access", "expires_in": 3600, "token_type": "Bearer"}
}

func TestExchangeRefreshTokenPresentsClientByAuthenticationMethod(t *testing.T) {
	for _, test := range []struct {
		name                 string
		method               ClientAuthenticationMethod
		expectedFormClientID string
		expectedFormSecret   string
		expectsBasicAuth     bool
	}{
		{name: "client secret post", method: ClientSecretPost, expectedFormClientID: "client-id", expectedFormSecret: "client-secret"},
		{name: "client secret basic", method: ClientSecretBasic, expectsBasicAuth: true},
		{name: "public client", method: PublicClient, expectedFormClientID: "client-id"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, testTokenEndpointURL, request.URL.String())
				require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
				require.Equal(t, "application/json", request.Header.Get("Accept"))
				require.NoError(t, request.ParseForm())
				require.Equal(t, "refresh_token", request.PostForm.Get("grant_type"))
				require.Equal(t, "existing-refresh", request.PostForm.Get("refresh_token"))
				require.Equal(t, test.expectedFormClientID, request.PostForm.Get("client_id"))
				require.Equal(t, test.expectedFormSecret, request.PostForm.Get("client_secret"))
				username, password, hasBasicAuth := request.BasicAuth()
				require.Equal(t, test.expectsBasicAuth, hasBasicAuth)
				if test.expectsBasicAuth {
					require.Equal(t, "client-id", username)
					require.Equal(t, "client-secret", password)
				}
				return tokenHTTPResponse(t, http.StatusOK, successfulTokenBody()), nil
			})
			token, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: test.method,
			}, sdkgo.NewSecretString("existing-refresh"))
			require.NoError(t, err)
			require.Equal(t, "new-access", token.AccessToken.Reveal())
		})
	}
}

func TestExchangeRefreshTokenReturnsRotationAndLifetimes(t *testing.T) {
	prior := sdkgo.NewSecretString("prior-refresh")
	for _, test := range []struct {
		name            string
		rotatedRefresh  string
		expectedRefresh string
	}{
		{name: "keeps prior refresh token when omitted", expectedRefresh: "prior-refresh"},
		{name: "returns rotated refresh token", rotatedRefresh: "rotated-refresh", expectedRefresh: "rotated-refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				body := successfulTokenBody()
				body["scope"] = "read write"
				body["refresh_token_expires_in"] = 15897600
				if test.rotatedRefresh != "" {
					body["refresh_token"] = test.rotatedRefresh
				}
				return tokenHTTPResponse(t, http.StatusOK, body), nil
			})
			token, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, prior)
			require.NoError(t, err)
			require.Equal(t, test.expectedRefresh, token.NextRefreshToken(prior).Reveal())
			require.Equal(t, time.Hour, token.ExpiresIn)
			require.Equal(t, 15897600*time.Second, token.RefreshTokenExpiresIn)
			require.Equal(t, "read write", token.Scope)
			require.Equal(t, "Bearer", token.TokenType)
		})
	}
}

func TestExchangeRefreshTokenRequiresMaterialWithoutCallingProvider(t *testing.T) {
	for _, test := range []struct {
		name         string
		client       ClientCredentials
		refreshToken string
	}{
		{name: "missing client ID", client: ClientCredentials{Secret: sdkgo.NewSecretString("secret"), AuthenticationMethod: ClientSecretPost}, refreshToken: "refresh"},
		{name: "missing refresh token", client: ClientCredentials{ID: "client-id", Secret: sdkgo.NewSecretString("secret"), AuthenticationMethod: ClientSecretPost}},
		{name: "post without secret", client: ClientCredentials{ID: "client-id", AuthenticationMethod: ClientSecretPost}, refreshToken: "refresh"},
		{name: "basic without secret", client: ClientCredentials{ID: "client-id", AuthenticationMethod: ClientSecretBasic}, refreshToken: "refresh"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				t.Fatal("incomplete refresh material must not call the provider")
				return nil, nil
			})
			_, err := endpoint.ExchangeRefreshToken(context.Background(), test.client, sdkgo.NewSecretString(test.refreshToken))
			require.Error(t, err)
			require.True(t, sdkgo.IsReauthorizationRequired(err))
		})
	}
}

func TestExchangeRefreshTokenRejectsUnknownAuthenticationMethod(t *testing.T) {
	endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
		t.Fatal("an unsupported client authentication method must not call the provider")
		return nil, nil
	})
	_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
		ID: "client-id", Secret: sdkgo.NewSecretString("secret"), AuthenticationMethod: "private_key_jwt",
	}, sdkgo.NewSecretString("refresh"))
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err))
}

func TestExchangeClassifiesProviderFailuresWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name                     string
		status                   int
		body                     string
		isReauthorization        bool
		expectedCauseMessagePart string
	}{
		{name: "terminal code on error status", status: http.StatusBadRequest,
			body: `{"error":"invalid_grant","error_description":"secret provider account detail"}`, isReauthorization: true,
			expectedCauseMessagePart: "Example rejected credential refresh with invalid_grant"},
		{name: "terminal code on success status", status: http.StatusOK,
			body: `{"error":"bad_refresh_token","error_description":"secret provider account detail"}`, isReauthorization: true,
			expectedCauseMessagePart: "Example rejected credential refresh with bad_refresh_token"},
		{name: "undeclared code is retryable and not repeated", status: http.StatusBadRequest,
			body:                     `{"error":"secret-looking-code","error_description":"secret provider account detail"}`,
			expectedCauseMessagePart: "Example token endpoint returned HTTP 400"},
		{name: "server failure is retryable", status: http.StatusServiceUnavailable,
			body: `{"error":"temporarily_unavailable"}`, expectedCauseMessagePart: "Example token endpoint returned HTTP 503"},
		{name: "terminal code during server failure is retryable", status: http.StatusBadGateway,
			body: `{"error":"invalid_grant"}`, expectedCauseMessagePart: "Example token endpoint returned HTTP 502"},
		{name: "terminal code on forbidden status", status: http.StatusForbidden,
			body: `{"error":"invalid_grant","error_description":"secret provider account detail"}`, isReauthorization: true,
			expectedCauseMessagePart: "Example rejected credential refresh with invalid_grant"},
		{name: "non JSON body is retryable", status: http.StatusBadGateway,
			body: `<html>secret provider account detail</html>`, expectedCauseMessagePart: "Example token response is invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})
			_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, sdkgo.NewSecretString("refresh-token"))
			require.Error(t, err)
			require.Equal(t, test.isReauthorization, sdkgo.IsReauthorizationRequired(err))
			require.Contains(t, errorTextWithCause(err), test.expectedCauseMessagePart)
			for _, secretText := range []string{"secret provider account detail", "secret-looking-code", "client-secret", "refresh-token"} {
				require.NotContains(t, errorTextWithCause(err), secretText)
			}
		})
	}
}

func TestExchangeRejectsIncompleteSuccessfulResponses(t *testing.T) {
	for _, test := range []struct {
		name     string
		body     map[string]any
		endpoint func(TokenEndpoint) TokenEndpoint
		isValid  bool
	}{
		{name: "missing access token", body: map[string]any{"expires_in": 3600, "token_type": "Bearer"}},
		{name: "non-positive lifetime", body: map[string]any{"access_token": "a", "expires_in": 0, "token_type": "Bearer"}},
		{name: "unexpected token type", body: map[string]any{"access_token": "a", "expires_in": 3600, "token_type": "mac"}},
		{name: "missing token type is rejected by default", body: map[string]any{"access_token": "a", "expires_in": 3600}},
		{name: "lowercase bearer is accepted", body: map[string]any{"access_token": "a", "expires_in": 3600, "token_type": "bearer"}, isValid: true},
		{name: "missing token type accepted when allowed", body: map[string]any{"access_token": "a", "expires_in": 3600},
			endpoint: func(endpoint TokenEndpoint) TokenEndpoint { endpoint.AcceptsMissingTokenType = true; return endpoint }, isValid: true},
		{name: "declared token type is accepted", body: map[string]any{"access_token": "a", "expires_in": 3600, "token_type": "bot"},
			endpoint: func(endpoint TokenEndpoint) TokenEndpoint {
				endpoint.AcceptedTokenTypes = []string{"bot"}
				return endpoint
			}, isValid: true},
		{name: "declared token types replace bearer", body: map[string]any{"access_token": "a", "expires_in": 3600, "token_type": "Bearer"},
			endpoint: func(endpoint TokenEndpoint) TokenEndpoint {
				endpoint.AcceptedTokenTypes = []string{"bot"}
				return endpoint
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				return tokenHTTPResponse(t, http.StatusOK, test.body), nil
			})
			if test.endpoint != nil {
				endpoint = test.endpoint(endpoint)
			}
			_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, sdkgo.NewSecretString("refresh-token"))
			if test.isValid {
				require.NoError(t, err)
				return
			}
			require.EqualError(t, err, "Example token response omitted required fields")
			require.False(t, sdkgo.IsReauthorizationRequired(err))
		})
	}
}

func TestExchangeValidatesEndpointBeforeCallingProvider(t *testing.T) {
	for _, test := range []struct {
		name            string
		url             string
		hasNoHTTPClient bool
	}{
		{name: "plain HTTP", url: "http://auth.example.test/oauth/token"},
		{name: "relative URL", url: "/oauth/token"},
		{name: "user information", url: "https://user:password@auth.example.test/oauth/token"},
		{name: "missing HTTP client", url: testTokenEndpointURL, hasNoHTTPClient: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				t.Fatal("an invalid endpoint must not call the provider")
				return nil, nil
			})
			endpoint.URL = test.url
			if test.hasNoHTTPClient {
				endpoint.HTTPClient = nil
			}
			_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, sdkgo.NewSecretString("refresh-token"))
			require.Error(t, err)
			require.NotContains(t, err.Error(), "password")
		})
	}
}

func TestExchangeDoesNotFollowRedirects(t *testing.T) {
	requestCount := 0
	endpoint := testTokenEndpoint(func(request *http.Request) (*http.Response, error) {
		requestCount++
		header := make(http.Header)
		header.Set("Location", "https://attacker.example.test/collect")
		return &http.Response{StatusCode: http.StatusTemporaryRedirect, Header: header, Body: io.NopCloser(strings.NewReader(`{}`)), Request: request}, nil
	})
	_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
		ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
	}, sdkgo.NewSecretString("refresh-token"))
	require.EqualError(t, err, "Example token endpoint returned HTTP 307")
	require.Equal(t, 1, requestCount)
}

func TestExchangeBoundsTokenResponseBody(t *testing.T) {
	endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
		oversized := `{"access_token":"` + strings.Repeat("a", maxTokenResponseBytes) + `"}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(oversized))}, nil
	})
	_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
		ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
	}, sdkgo.NewSecretString("refresh-token"))
	require.EqualError(t, err, "Example token response could not be read")
}

func TestExchangeReportsUnavailableEndpointAsRetryable(t *testing.T) {
	endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection reset")
	})
	_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
		ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
	}, sdkgo.NewSecretString("refresh-token"))
	require.ErrorContains(t, err, "Example token endpoint is unavailable")
	require.False(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), "client-secret")
	require.NotContains(t, err.Error(), "refresh-token")
}

func TestExchangeJWTBearerAssertionSendsOnlyTheAssertion(t *testing.T) {
	endpoint := testTokenEndpoint(func(request *http.Request) (*http.Response, error) {
		require.NoError(t, request.ParseForm())
		require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", request.PostForm.Get("grant_type"))
		require.Equal(t, "signed.assertion.value", request.PostForm.Get("assertion"))
		require.Len(t, request.PostForm, 2)
		_, _, hasBasicAuth := request.BasicAuth()
		require.False(t, hasBasicAuth)
		return tokenHTTPResponse(t, http.StatusOK, successfulTokenBody()), nil
	})
	token, err := endpoint.ExchangeJWTBearerAssertion(context.Background(), sdkgo.NewSecretString("signed.assertion.value"))
	require.NoError(t, err)
	require.Equal(t, "new-access", token.AccessToken.Reveal())
	require.Empty(t, token.RefreshToken.Reveal())

	_, err = endpoint.ExchangeJWTBearerAssertion(context.Background(), sdkgo.SecretString{})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

// errorTextWithCause returns err's text followed by its causes' text, because
// a reauthorization error's own text is deliberately generic.
func errorTextWithCause(err error) string {
	text := err.Error()
	if multiple, isMultiple := err.(interface{ Unwrap() []error }); isMultiple {
		for _, cause := range multiple.Unwrap() {
			text += ": " + cause.Error()
		}
	}
	return text
}

func TestExchangeAcceptsMissingExpiresInOnlyWhenDeclared(t *testing.T) {
	for _, test := range []struct {
		name                    string
		body                    map[string]any
		acceptsMissingExpiresIn bool
		isValid                 bool
	}{
		{name: "missing lifetime rejected by default", body: map[string]any{"access_token": "a", "token_type": "Bearer"}},
		{name: "missing lifetime accepted when declared", body: map[string]any{"access_token": "a", "token_type": "Bearer"}, acceptsMissingExpiresIn: true, isValid: true},
		{name: "negative lifetime rejected even when missing is accepted", body: map[string]any{"access_token": "a", "token_type": "Bearer", "expires_in": -1}, acceptsMissingExpiresIn: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				return tokenHTTPResponse(t, http.StatusOK, test.body), nil
			})
			endpoint.AcceptsMissingExpiresIn = test.acceptsMissingExpiresIn
			token, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, sdkgo.NewSecretString("refresh-token"))
			if !test.isValid {
				require.EqualError(t, err, "Example token response omitted required fields")
				return
			}
			require.NoError(t, err)
			require.Zero(t, token.ExpiresIn)
		})
	}
}

func TestExchangeRetainsOnlyDeclaredResponseFields(t *testing.T) {
	endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
		body := successfulTokenBody()
		body["instance_url"] = "https://example.my.salesforce.test"
		body["signature"] = "undeclared"
		body["team"] = map[string]any{"id": "T1"}
		return tokenHTTPResponse(t, http.StatusOK, body), nil
	})
	endpoint.RetainedResponseFields = []string{"instance_url", "team", "api_domain"}
	token, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
		ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
	}, sdkgo.NewSecretString("refresh-token"))
	require.NoError(t, err)
	require.Equal(t, map[string]json.RawMessage{
		"instance_url": json.RawMessage(`"https://example.my.salesforce.test"`),
		"team":         json.RawMessage(`{"id":"T1"}`),
	}, token.RetainedFields)

	endpoint.RetainedResponseFields = nil
	token, err = endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
		ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
	}, sdkgo.NewSecretString("refresh-token"))
	require.NoError(t, err)
	require.Nil(t, token.RetainedFields)
}

func TestExchangeRefusesToRetainStandardTokenFields(t *testing.T) {
	for _, fieldName := range []string{"access_token", "refresh_token", "id_token", "error_description"} {
		t.Run(fieldName, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				t.Fatal("an invalid retained field must not call the provider")
				return nil, nil
			})
			endpoint.RetainedResponseFields = []string{"instance_url", fieldName}
			_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, sdkgo.NewSecretString("refresh-token"))
			require.EqualError(t, err, "Example token endpoint cannot retain the standard "+fieldName+" field")
		})
	}
}

func TestExchangeEncodesJSONRequestBodyWithAdditionalParameters(t *testing.T) {
	for _, test := range []struct {
		name                string
		usesJSONRequestBody bool
	}{
		{name: "form body"},
		{name: "JSON body", usesJSONRequestBody: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				parameters := map[string]string{}
				if test.usesJSONRequestBody {
					require.Equal(t, "application/json", request.Header.Get("Content-Type"))
					require.NoError(t, json.Unmarshal(body, &parameters))
				} else {
					require.Equal(t, "application/x-www-form-urlencoded", request.Header.Get("Content-Type"))
					form, err := url.ParseQuery(string(body))
					require.NoError(t, err)
					for parameterName := range form {
						parameters[parameterName] = form.Get(parameterName)
					}
				}
				require.Equal(t, map[string]string{
					"grant_type": "refresh_token", "refresh_token": "refresh-token",
					"client_id": "client-id", "client_secret": "client-secret", "expires_in": "172800",
				}, parameters)
				return tokenHTTPResponse(t, http.StatusOK, successfulTokenBody()), nil
			})
			endpoint.UsesJSONRequestBody = test.usesJSONRequestBody
			endpoint.AdditionalRequestParameters = map[string]string{"expires_in": "172800"}
			_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, sdkgo.NewSecretString("refresh-token"))
			require.NoError(t, err)
		})
	}
}

func TestExchangeRefusesAdditionalParametersThatReplaceGrantParameters(t *testing.T) {
	for _, parameterName := range []string{"grant_type", "refresh_token", "client_secret", "assertion"} {
		t.Run(parameterName, func(t *testing.T) {
			endpoint := testTokenEndpoint(func(*http.Request) (*http.Response, error) {
				t.Fatal("a replacing parameter must not call the provider")
				return nil, nil
			})
			endpoint.AdditionalRequestParameters = map[string]string{parameterName: "attacker-value"}
			_, err := endpoint.ExchangeRefreshToken(context.Background(), ClientCredentials{
				ID: "client-id", Secret: sdkgo.NewSecretString("client-secret"), AuthenticationMethod: ClientSecretPost,
			}, sdkgo.NewSecretString("refresh-token"))
			require.EqualError(t, err, "Example token endpoint cannot replace the "+parameterName+" request parameter")
		})
	}
}
