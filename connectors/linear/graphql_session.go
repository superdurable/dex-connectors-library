// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// graphQLSession runs one operation's requests under one deadline and one credential resolution.
type graphQLSession struct {
	client       *Client
	call         sdkgo.Call
	ctx          context.Context
	operation    string
	credentials  Credentials
	hasRefreshed bool
}

// openSession resolves credentials once; a failure is returned as the operation's classified exchange.
func (client *Client) openSession(call sdkgo.Call, operation string) (*graphQLSession, context.CancelFunc, *linearExchange) {
	ctx, cancel := context.WithTimeout(call.Context, operationTimeout)
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	var failed *linearExchange
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		failed = &linearExchange{outcome: exchangeRejected,
			failure: linearFailure(sdkgo.FailureAuthentication, operation, "Linear authorization must be renewed in Dex Web Connections")}
	case errors.Is(err, errCredentialRefreshUnavailable), errors.Is(err, projectconfig.ErrRefreshFailed):
		failed = &linearExchange{outcome: exchangeRetry,
			failure: linearFailure(sdkgo.FailureAvailability, operation, "Linear OAuth token refresh is temporarily unavailable")}
	case err != nil || validateResolvedCredentials(credentials) != nil:
		failed = &linearExchange{outcome: exchangeDefect,
			failure: linearFailure(sdkgo.FailureAuthentication, operation, "Linear connection credentials are unavailable or unusable")}
	}
	if failed != nil {
		cancel()
		return nil, func() {}, failed
	}
	return &graphQLSession{client: client, call: call, ctx: ctx, operation: operation, credentials: credentials}, cancel, nil
}

// exchange sends request and resends it once after Linear rejects an OAuth token; a 401 means Linear ran nothing.
func (session *graphQLSession) exchange(request graphQLRequest) linearExchange {
	result := session.send(request)
	if result.response.statusCode != http.StatusUnauthorized || session.hasRefreshed || !session.canRefreshAfterRejection() {
		return result
	}
	session.hasRefreshed = true
	replacement, err := sdkgo.ResolveCredentialAfterRejection(session.ctx, session.client.credentials, session.call, session.client.refreshDriver)
	if err != nil || validateResolvedCredentials(replacement) != nil {
		return result
	}
	session.credentials = replacement
	return session.send(request)
}

func (session *graphQLSession) canRefreshAfterRejection() bool {
	if session.credentials.AuthMethodID != OAuthAuthMethodID {
		return false
	}
	_, supportsRejectionRefresh := session.client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return supportsRejectionRefresh
}

// send performs one HTTP exchange and classifies it without deciding branch policy.
func (session *graphQLSession) send(request graphQLRequest) linearExchange {
	client, operation := session.client, session.operation
	httpRequest, err := session.buildRequest(request)
	if err != nil {
		return linearExchange{outcome: exchangeDefect, failure: linearFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return linearExchange{outcome: exchangeRetry, failure: linearFailure(sdkgo.FailureTransport, operation, "Linear could not be reached; no request was sent")}
		}
		return linearExchange{outcome: exchangeRetry, failure: linearFailure(sdkgo.FailureTransport, operation, "Linear request failed before a response arrived")}
	}
	body, readErr := providerhttp.ReadBoundedBody(httpResponse.Body, client.maxResponseBytes)
	closeErr := httpResponse.Body.Close()
	response := linearResponse{statusCode: httpResponse.StatusCode}
	if requestID := strings.TrimSpace(httpResponse.Header.Get(requestIDHeader)); requestIDPattern.MatchString(requestID) {
		response.requestID = requestID
	}
	isSuccess := httpResponse.StatusCode >= 200 && httpResponse.StatusCode < 300
	switch {
	case errors.Is(readErr, providerhttp.ErrBodyTooLarge) && isSuccess:
		return linearExchange{outcome: exchangeInvalid, response: response,
			failure: linearFailure(sdkgo.FailureResponseTooLarge, operation, "Linear response exceeds the configured maxResponseBytes limit")}
	case readErr != nil && !errors.Is(readErr, providerhttp.ErrBodyTooLarge), closeErr != nil:
		return linearExchange{outcome: exchangeRetry, response: response, failure: linearFailure(sdkgo.FailureTransport, operation, "Linear response could not be read")}
	}
	secret := session.credentials.authorizationToken().Reveal()
	if bytes.Contains(body, []byte(secret)) {
		return linearExchange{outcome: exchangeInvalid, response: response,
			failure: linearFailure(sdkgo.FailureProtocol, operation, "Linear response reflected the connection credential")}
	}
	retryAfter := providerhttp.ParseRetryAfter(httpResponse.Header.Get("Retry-After"), client.now())
	var envelope struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		if isSuccess {
			return linearExchange{outcome: exchangeInvalid, response: response,
				failure: linearFailure(sdkgo.FailureProtocol, operation, "Linear returned a response that is not a GraphQL JSON object")}
		}
		return classifyFailedResponse(operation, response, graphQLErrorSummary{}, retryAfter)
	}
	if !isJSONNull(envelope.Data) {
		response.data = envelope.Data
	}
	if len(envelope.Errors) != 0 || !isSuccess {
		return classifyFailedResponse(operation, response, summarizeGraphQLErrors(body, secret), retryAfter)
	}
	if response.data == nil {
		return linearExchange{outcome: exchangeInvalid, response: response, failure: linearFailure(sdkgo.FailureProtocol, operation, "Linear returned neither data nor errors")}
	}
	return linearExchange{outcome: exchangeSucceeded, response: response}
}

func (session *graphQLSession) buildRequest(request graphQLRequest) (*http.Request, error) {
	encoded, err := json.Marshal(graphQLRequestBody{OperationName: request.operationName, Query: request.document, Variables: request.variables})
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest, err := http.NewRequestWithContext(session.ctx, http.MethodPost, session.client.apiURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", session.credentials.authorizationHeader())
	return httpRequest, nil
}

// receipt carries the Call ID, the Linear request ID, and the object the operation named or wrote.
func (session *graphQLSession) receipt(response linearResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: session.call.ID, IdempotencyKey: session.call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: response.requestID, ObservedAt: session.client.now().UTC(),
	}
}

// authorizationToken returns the secret the selected authentication method sends.
func (credentials Credentials) authorizationToken() sdkgo.SecretString {
	if credentials.AuthMethodID == OAuthAuthMethodID {
		return credentials.AccessToken
	}
	return credentials.APIKey
}

// authorizationHeader sends a personal API key raw, because Linear rejects one behind Bearer.
func (credentials Credentials) authorizationHeader() string {
	if credentials.AuthMethodID == OAuthAuthMethodID {
		return "Bearer " + credentials.AccessToken.Reveal()
	}
	return credentials.APIKey.Reveal()
}

// validateResolvedCredentials checks only what a request needs, so credentials without renewal material pass.
func validateResolvedCredentials(credentials Credentials) error {
	switch credentials.AuthMethodID {
	case PersonalAPIKeyAuthMethodID, OAuthAuthMethodID:
	default:
		return errors.New("Linear auth_method is invalid")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.authorizationToken().Reveal()) {
		return errors.New("Linear API key or access token must be printable ASCII without spaces")
	}
	return nil
}

// isConnectionNeverEstablished reports a dial failure, after which Linear cannot have received the request.
func isConnectionNeverEstablished(err error) bool {
	var operationError *net.OpError
	return errors.As(err, &operationError) && operationError.Op == "dial"
}

func isJSONNull(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

func linearFailure(kind sdkgo.FailureKind, operation string, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}

func linearFailurePointer(kind sdkgo.FailureKind, operation string, message string) *sdkgo.Failure {
	failure := linearFailure(kind, operation, message)
	return &failure
}
