// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"regexp"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	requestIDHeader = "X-Request-Id"
	// minimumRedactedSecretBytes keeps a short test value from rewriting every response.
	minimumRedactedSecretBytes = 8
	maximumContentTypeBytes    = 256
	redactedText               = "[REDACTED]"
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// SendRequestInput describes one HTTP request to the connection's API.
type SendRequestInput struct {
	// Method is GET, HEAD, OPTIONS, POST, PUT, PATCH, or DELETE, compared without case. Blank means GET.
	Method string `json:"method,omitempty"`
	// Path is the request path under the connection's apiBaseUrl. It starts with a single slash, may be
	// percent-encoded, and carries no query, fragment, backslash, or .. segment. Set exactly one of Path and URL.
	Path string `json:"path,omitempty"`
	// URL is an absolute http or https URL with the scheme, host, and port of the connection's apiBaseUrl,
	// such as a next-page link the API returned. Set exactly one of Path and URL.
	URL string `json:"url,omitempty"`
	// Query holds query parameters that are added to those already in URL. A name may repeat through several values.
	Query map[string][]string `json:"query,omitempty"`
	// Headers holds extra request headers, such as Accept. Authorization, Cookie, Host, and the headers the
	// transport owns select defect; the connector always sends Authorization: Bearer with the access token.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is the request body, sent as text. It is not allowed with GET or HEAD and is limited to the
	// connection's maxBodyBytes. Content-Type defaults to application/json when Headers sets none.
	Body string `json:"body,omitempty"`
	// IdempotencyKeyHeader names a header, such as Idempotency-Key, that carries the Step's idempotency key,
	// which is the same for every retry of one Step execution. Blank sends none.
	IdempotencyKeyHeader string `json:"idempotencyKeyHeader,omitempty"`
}

// SendRequestOutput is the API's answer. It never holds a credential: the access token is replaced by
// [REDACTED] if the API echoes it.
type SendRequestOutput struct {
	// StatusCode is the final HTTP status, or zero when no request was sent or none was answered.
	StatusCode int `json:"statusCode"`
	// ContentType is the response Content-Type header, when it is short printable text.
	ContentType string `json:"contentType,omitempty"`
	// Body is the response body, cut at the connection's maxBodyBytes. It is base64 text when IsBodyBase64 is set.
	Body string `json:"body,omitempty"`
	// IsBodyBase64 reports that the body was not valid UTF-8, so Body holds its base64 encoding.
	IsBodyBase64 bool `json:"bodyIsBase64,omitempty"`
	// IsBodyTruncated reports that the API sent more than maxBodyBytes bytes and the rest was dropped.
	IsBodyTruncated bool `json:"bodyTruncated,omitempty"`
}

// SendRequestOperation implements the sendRequest Mutation. Build it with Client.SendRequest.
type SendRequestOperation struct{ client *Client }

// Definition returns the immutable sendRequest operation definition.
func (SendRequestOperation) Definition() sdkgo.MutationDefinition { return SendRequestDefinition }

// IdempotencyKey is the stable Call ID, which every attempt of one Step execution sends in the input's
// idempotencyKeyHeader.
func (SendRequestOperation) IdempotencyKey(callID sdkgo.CallID, _ SendRequestInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends the request once with the access token, and again after a 401 once the token has been
// renewed. A 2xx selects completed, and a redirect, a 4xx other than 408 and 429, or an authorization the
// server will no longer renew selects rejected. A 408 or 429, a request that never left this process, and
// a failed GET, HEAD, or OPTIONS return Retry. A POST, PUT, PATCH, or DELETE that was written but got no
// usable answer selects uncertain.
func (operation SendRequestOperation) Invoke(call sdkgo.Call, input SendRequestInput) sdkgo.MutationAttempt[SendRequestOutput] {
	client := operation.client
	request, err := client.planRequest(input)
	if err != nil {
		return sendRequestBranch(SendRequestBranchDefect, SendRequestOutput{}, sdkgo.FailureValidation, err.Error(), sdkgo.Receipt{})
	}
	ctx, cancel := context.WithTimeout(call.Context, operationTimeout)
	defer cancel()
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	if attempt, isFailed := operation.credentialFailure(ctx, err, credentials); isFailed {
		return attempt
	}
	answer := client.exchange(ctx, call, request, credentials)
	usedTokens := []string{credentials.AccessToken.Reveal()}
	if answer.statusCode == http.StatusUnauthorized && answer.err == nil {
		replacement, refreshErr := sdkgo.ResolveCredentialAfterRejection(ctx, client.credentials, call, client.refreshDriver)
		if refreshErr == nil && replacement.AccessToken.Reveal() != "" && replacement.AccessToken.Reveal() != credentials.AccessToken.Reveal() {
			answer = client.exchange(ctx, call, request, replacement)
			usedTokens = append(usedTokens, replacement.AccessToken.Reveal())
		}
	}
	return operation.attemptFor(call, request, answer, usedTokens)
}

// credentialFailure maps a failed credential resolution to its attempt. It reports isFailed false when
// the credentials are usable.
func (operation SendRequestOperation) credentialFailure(
	ctx context.Context, err error, credentials Credentials,
) (sdkgo.MutationAttempt[SendRequestOutput], bool) {
	var refusal *providerhttp.AddressRefusal
	switch {
	case errors.As(err, &refusal):
		return sendRequestBranch(SendRequestBranchDefect, SendRequestOutput{}, sdkgo.FailureValidation,
			describeRefusal("tokenEndpoint", refusal), sdkgo.Receipt{}), true
	case sdkgo.IsReauthorizationRequired(err):
		return sendRequestBranch(SendRequestBranchRejected, SendRequestOutput{}, sdkgo.FailureAuthentication,
			"the OAuth authorization must be renewed in Dex Web Connections", sdkgo.Receipt{}), true
	case errors.Is(err, errCredentialRefreshUnavailable), errors.Is(err, projectconfig.ErrRefreshFailed), ctx.Err() != nil:
		return sdkgo.NewMutationRetry[SendRequestOutput](sendRequestFailure(sdkgo.FailureAvailability,
			"the OAuth access token could not be refreshed now"), 0), true
	case err != nil || credentials.Validate() != nil:
		return sendRequestBranch(SendRequestBranchDefect, SendRequestOutput{}, sdkgo.FailureAuthentication,
			"the connection's credentials are unavailable or unusable", sdkgo.Receipt{}), true
	}
	return sdkgo.MutationAttempt[SendRequestOutput]{}, false
}

// httpExchange is one request and its answer, before any branch is chosen.
type httpExchange struct {
	statusCode      int
	header          http.Header
	body            []byte
	isBodyTruncated bool
	// err is a failure to build, send, or fully read the exchange; isDispatched tells whether the request
	// was written to the connection before it.
	err          error
	isDispatched bool
	// refusal is set when the address policy refused the connection, so no request was sent.
	refusal *providerhttp.AddressRefusal
}

// exchange sends the planned request once with credentials' access token. It reads at most maxBodyBytes+1
// bytes of the answer.
func (client *Client) exchange(ctx context.Context, call sdkgo.Call, request plannedRequest, credentials Credentials) httpExchange {
	var isDispatched atomic.Bool
	tracedContext := httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteRequest: func(written httptrace.WroteRequestInfo) {
			if written.Err == nil {
				isDispatched.Store(true)
			}
		},
	})
	var body io.Reader = http.NoBody
	if request.body != "" {
		body = strings.NewReader(request.body)
	}
	httpRequest, err := http.NewRequestWithContext(tracedContext, request.method, request.target.String(), body)
	if err != nil {
		return httpExchange{err: errors.New("the request could not be built")}
	}
	httpRequest.Header = request.headers.Clone()
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	if httpRequest.Header.Get("User-Agent") == "" {
		httpRequest.Header.Set("User-Agent", userAgent)
	}
	if request.idempotencyKeyHeader != "" {
		httpRequest.Header.Set(request.idempotencyKeyHeader, string(call.IdempotencyKey))
	}
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		var refusal *providerhttp.AddressRefusal
		if errors.As(err, &refusal) {
			return httpExchange{err: errors.New("the connection was refused"), refusal: refusal}
		}
		return httpExchange{err: errors.New("the request failed before an answer arrived"), isDispatched: isDispatched.Load()}
	}
	defer response.Body.Close()
	contents, readErr := io.ReadAll(io.LimitReader(response.Body, client.maxBodyBytes+1))
	answer := httpExchange{statusCode: response.StatusCode, header: response.Header, body: contents, isDispatched: true}
	if int64(len(contents)) > client.maxBodyBytes {
		answer.body, answer.isBodyTruncated = trimIncompleteRune(contents[:client.maxBodyBytes]), true
	}
	if readErr != nil {
		answer.err = errors.New("the answer was cut off")
	}
	return answer
}

// attemptFor chooses the branch for one finished exchange.
func (operation SendRequestOperation) attemptFor(
	call sdkgo.Call, request plannedRequest, answer httpExchange, usedTokens []string,
) sdkgo.MutationAttempt[SendRequestOutput] {
	client := operation.client
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: ConnectorID,
		ProviderRequestID: safeRequestID(answer.header), ObservedAt: client.now().UTC(),
	}
	output := responseOutput(answer, usedTokens)
	status := answer.statusCode
	switch {
	case answer.refusal != nil:
		return sendRequestBranch(SendRequestBranchDefect, SendRequestOutput{}, sdkgo.FailureValidation,
			describeRefusal("apiBaseUrl", answer.refusal), sdkgo.Receipt{})
	case answer.err != nil && !answer.isDispatched:
		return sdkgo.NewMutationRetry[SendRequestOutput](sendRequestFailure(sdkgo.FailureAvailability,
			"the API could not be reached; no request was sent"), 0)
	case answer.err != nil && request.isSafeToRepeat():
		return sdkgo.NewMutationRetry[SendRequestOutput](sendRequestFailure(sdkgo.FailureTransport,
			"the API did not answer the "+request.method+" request"), 0)
	case answer.err != nil:
		return sdkgo.NewMutationUncertain(output, sendRequestFailure(sdkgo.FailureTransport,
			"the request was sent but the API did not answer completely"), receipt)
	case status >= 200 && status < 300:
		return sdkgo.NewMutationBranch(SendRequestBranchCompleted, output, nil, receipt)
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[SendRequestOutput](sendRequestFailure(sdkgo.FailureRateLimit,
			fmt.Sprintf("the API answered HTTP %d", status)), providerhttp.ParseRetryAfter(answer.header.Get("Retry-After"), client.now()))
	case status >= 500 && request.isSafeToRepeat():
		return sdkgo.NewMutationRetry[SendRequestOutput](sendRequestFailure(sdkgo.FailureAvailability,
			fmt.Sprintf("the API answered HTTP %d", status)), providerhttp.ParseRetryAfter(answer.header.Get("Retry-After"), client.now()))
	case status >= 500:
		return sdkgo.NewMutationUncertain(output, sendRequestFailure(sdkgo.FailureAvailability,
			fmt.Sprintf("the API answered HTTP %d after the request was sent", status)), receipt)
	default:
		failure := sendRequestFailure(rejectionFailureKind(status), fmt.Sprintf("the API answered HTTP %d", status))
		return sdkgo.NewMutationBranch(SendRequestBranchRejected, output, &failure, receipt)
	}
}

// responseOutput turns an answer into the Result value: the access tokens are redacted from the body, and
// a body that is not valid UTF-8 is base64-encoded.
func responseOutput(exchange httpExchange, usedTokens []string) SendRequestOutput {
	output := SendRequestOutput{StatusCode: exchange.statusCode, IsBodyTruncated: exchange.isBodyTruncated}
	if contentType := exchange.header.Get("Content-Type"); len(contentType) <= maximumContentTypeBytes && isPrintableASCII(contentType) {
		output.ContentType = contentType
	}
	body := exchange.body
	for _, token := range usedTokens {
		if len(token) >= minimumRedactedSecretBytes {
			body = bytes.ReplaceAll(body, []byte(token), []byte(redactedText))
		}
	}
	if utf8.Valid(body) {
		output.Body = string(body)
	} else {
		output.Body, output.IsBodyBase64 = base64.StdEncoding.EncodeToString(body), true
	}
	return output
}

// trimIncompleteRune drops the bytes of a UTF-8 character that a size limit cut in half.
func trimIncompleteRune(body []byte) []byte {
	for trimmed := 0; trimmed < utf8.UTFMax && len(body) > 0; trimmed++ {
		lastRune, size := utf8.DecodeLastRune(body)
		if lastRune != utf8.RuneError || size != 1 {
			return body
		}
		body = body[:len(body)-1]
	}
	return body
}

func isPrintableASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < ' ' || value[index] > '~' {
			return false
		}
	}
	return true
}

func safeRequestID(header http.Header) string {
	if requestID := strings.TrimSpace(header.Get(requestIDHeader)); requestIDPattern.MatchString(requestID) {
		return requestID
	}
	return ""
}

func rejectionFailureKind(status int) sdkgo.FailureKind {
	switch status {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	case http.StatusNotFound, http.StatusGone:
		return sdkgo.FailureNotFound
	case http.StatusConflict:
		return sdkgo.FailureConflict
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

// sendRequestBranch returns a branch attempt that carries a failure.
func sendRequestBranch(
	branch sdkgo.BranchID, output SendRequestOutput, kind sdkgo.FailureKind, message string, receipt sdkgo.Receipt,
) sdkgo.MutationAttempt[SendRequestOutput] {
	failure := sendRequestFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, output, &failure, receipt)
}
