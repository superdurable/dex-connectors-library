// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package square provides read-only Square Query operations for Dex Flows.
package square

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials for each request.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the connection settings without contacting Square.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Square credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, err
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListPaymentsInput selects a Square payment page.
type ListPaymentsInput struct {
	// Cursor continues a previous page; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
}

// GetPaymentInput identifies one Square payment.
type GetPaymentInput struct {
	// PaymentID is the Square payment ID.
	PaymentID string `json:"paymentId"`
}

// Money is a Square integer amount in the currency's smallest denomination.
type Money struct {
	// Amount is an integer count of the currency's smallest unit.
	Amount int64 `json:"amount"`
	// Currency is the ISO currency code.
	Currency string `json:"currency"`
}

// Payment is a minimal Square payment summary.
type Payment struct {
	// ID is the Square payment ID.
	ID string `json:"id"`
	// Status is the provider payment status.
	Status string `json:"status"`
	// AmountMoney is the charged amount.
	AmountMoney Money `json:"amount_money"`
}

// PaymentPage is one Square payment page.
type PaymentPage struct {
	// Payments contains payments, possibly none.
	Payments []Payment `json:"payments"`
	// Cursor continues listing when set.
	Cursor string `json:"cursor"`
}

// ListPaymentsOperation executes the listPayments read.
type ListPaymentsOperation struct{ client *Client }

// ListPayments returns the listPayments Query bound to this client.
func (client *Client) ListPayments() ListPaymentsOperation {
	return ListPaymentsOperation{client: client}
}

// Definition returns the manifest-generated listPayments contract.
func (ListPaymentsOperation) Definition() sdkgo.QueryDefinition { return ListPaymentsDefinition }

// Invoke reads the provider resource and classifies the response.
func (operation ListPaymentsOperation) Invoke(call sdkgo.Call, input ListPaymentsInput) sdkgo.QueryAttempt[PaymentPage] {
	query := url.Values{}
	path := "/payments"
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	return executeRead(operation.client, call, "listPayments", path, query, ListPaymentsBranchListed, ListPaymentsBranchNotFound, ListPaymentsBranchProviderRejected, ListPaymentsBranchInvalidResponse, ListPaymentsBranchDefect, decodeListPaymentsResponse)
}

func decodeListPaymentsResponse(body []byte) (PaymentPage, error) {
	var response PaymentPage
	err := json.Unmarshal(body, &response)
	if err != nil {
		return PaymentPage{}, err
	}
	return response, nil
}

// GetPaymentOperation executes the getPayment read.
type GetPaymentOperation struct{ client *Client }

// GetPayment returns the getPayment Query bound to this client.
func (client *Client) GetPayment() GetPaymentOperation { return GetPaymentOperation{client: client} }

// Definition returns the manifest-generated getPayment contract.
func (GetPaymentOperation) Definition() sdkgo.QueryDefinition { return GetPaymentDefinition }

// Invoke reads the provider resource and classifies the response.
func (operation GetPaymentOperation) Invoke(call sdkgo.Call, input GetPaymentInput) sdkgo.QueryAttempt[Payment] {
	query := url.Values{}
	if !validPathSegment(input.PaymentID) {
		return failedAttempt[Payment]("getPayment", GetPaymentBranchDefect, sdkgo.FailureValidation, "paymentId must be a path segment")
	}
	path := "/payments/" + url.PathEscape(input.PaymentID)
	return executeRead(operation.client, call, "getPayment", path, query, GetPaymentBranchFound, GetPaymentBranchNotFound, GetPaymentBranchProviderRejected, GetPaymentBranchInvalidResponse, GetPaymentBranchDefect, decodeGetPaymentResponse)
}

func decodeGetPaymentResponse(body []byte) (Payment, error) {
	var response struct {
		Payment Payment `json:"payment"`
	}
	err := json.Unmarshal(body, &response)
	if err != nil || response.Payment.ID == "" {
		return Payment{}, fmt.Errorf("missing payment")
	}
	return response.Payment, nil
}

type authResult struct {
	failure    *sdkgo.Failure
	branch     sdkgo.BranchID
	retry      bool
	retryAfter time.Duration
}

func (client *Client) authorize(call sdkgo.Call, operation string, request *http.Request, defectBranch, rejectedBranch, invalidBranch sdkgo.BranchID) authResult {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return authResult{failure: failureFor(operation, sdkgo.FailureAuthentication, "Square credentials are unavailable"), branch: defectBranch}
	}
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("Square-Version", "2026-09-16")
	return authResult{}
}

func executeRead[T any](client *Client, call sdkgo.Call, operation, path string, query url.Values, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	endpoint := client.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return failedAttempt[T](operation, defectBranch, sdkgo.FailureLocalDefect, "provider request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, operation, request, defectBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		if authorization.retry {
			return sdkgo.NewQueryRetry[T](*authorization.failure, authorization.retryAfter)
		}
		return sdkgo.NewQueryBranch(authorization.branch, *new(T), authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider read failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureRateLimit, "provider rate limited the read"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider read is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return failedAttempt[T](operation, notFoundBranch, sdkgo.FailureProviderRejection, "resource was not found")
	case response.StatusCode == http.StatusUnauthorized:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureAuthentication, "provider rejected credentials")
	case response.StatusCode == http.StatusForbidden:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureAuthorization, "provider denied access")
	case response.StatusCode != http.StatusOK:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureProviderRejection, "provider rejected read with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedAttempt[T](operation, invalidBranch, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(body)
	if err != nil {
		return failedAttempt[T](operation, invalidBranch, sdkgo.FailureProtocol, "provider returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}

func validPathSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/?#") && strings.IndexFunc(value, func(character rune) bool { return character < 32 }) < 0
}

func failedAttempt[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryBranch(branch, *new(T), failureFor(operation, kind, message), sdkgo.Receipt{})
}
func failureFor(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operation, Message: message}
}
