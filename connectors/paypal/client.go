// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package paypal provides read-only PayPal Query operations for Dex Flows.
package paypal

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

// New validates the connection settings without contacting PayPal.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("PayPal credential source is required")
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

// GetOrderInput identifies one PayPal checkout order.
type GetOrderInput struct {
	// OrderID is the PayPal checkout order ID.
	OrderID string `json:"orderId"`
}

// ListTransactionsInput selects a PayPal transaction window and page.
type ListTransactionsInput struct {
	// StartDate is the inclusive RFC 3339 window start.
	StartDate string `json:"startDate"`
	// EndDate is the inclusive RFC 3339 window end.
	EndDate string `json:"endDate"`
	// Page is the one-based page number; zero selects page one.
	Page int `json:"page,omitempty"`
}

// Order is a minimal PayPal checkout order summary.
type Order struct {
	// ID is the PayPal order ID.
	ID string `json:"id"`
	// Status is the PayPal order state.
	Status string `json:"status"`
}

// TransactionAmount is the decimal amount and ISO currency.
type TransactionAmount struct {
	// CurrencyCode is the ISO currency code.
	CurrencyCode string `json:"currency_code"`
	// Value is the decimal amount.
	Value string `json:"value"`
}

// TransactionInfo is the transaction ID, status, and amount.
type TransactionInfo struct {
	// TransactionID is the PayPal transaction ID.
	TransactionID string `json:"transaction_id"`
	// TransactionStatus is the PayPal status code.
	TransactionStatus string `json:"transaction_status"`
	// TransactionAmount is the transaction amount.
	TransactionAmount TransactionAmount `json:"transaction_amount"`
}

// TransactionDetail contains the non-secret transaction summary.
type TransactionDetail struct {
	// TransactionInfo contains the provider transaction summary.
	TransactionInfo TransactionInfo `json:"transaction_info"`
}

// TransactionPage is one PayPal transaction search page.
type TransactionPage struct {
	// TransactionDetails contains transactions, possibly none.
	TransactionDetails []TransactionDetail `json:"transaction_details"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// TotalPages is the provider's count of pages.
	TotalPages int `json:"total_pages"`
}

// GetOrderOperation executes the getOrder read.
type GetOrderOperation struct{ client *Client }

// GetOrder returns the getOrder Query bound to this client.
func (client *Client) GetOrder() GetOrderOperation { return GetOrderOperation{client: client} }

// Definition returns the manifest-generated getOrder contract.
func (GetOrderOperation) Definition() sdkgo.QueryDefinition { return GetOrderDefinition }

// Invoke reads the provider resource and classifies the response.
func (operation GetOrderOperation) Invoke(call sdkgo.Call, input GetOrderInput) sdkgo.QueryAttempt[Order] {
	query := url.Values{}
	if !validPathSegment(input.OrderID) {
		return failedAttempt[Order]("getOrder", GetOrderBranchDefect, sdkgo.FailureValidation, "orderId must be a path segment")
	}
	path := "/v2/checkout/orders/" + url.PathEscape(input.OrderID)
	return executeRead(operation.client, call, "getOrder", path, query, GetOrderBranchFound, GetOrderBranchNotFound, GetOrderBranchProviderRejected, GetOrderBranchInvalidResponse, GetOrderBranchDefect, decodeGetOrderResponse)
}

func decodeGetOrderResponse(body []byte) (Order, error) {
	var response Order
	err := json.Unmarshal(body, &response)
	if err != nil {
		return Order{}, err
	}
	if response.ID == "" {
		return Order{}, fmt.Errorf("missing order")
	}
	return response, nil
}

// ListTransactionsOperation executes the listTransactions read.
type ListTransactionsOperation struct{ client *Client }

// ListTransactions returns the listTransactions Query bound to this client.
func (client *Client) ListTransactions() ListTransactionsOperation {
	return ListTransactionsOperation{client: client}
}

// Definition returns the manifest-generated listTransactions contract.
func (ListTransactionsOperation) Definition() sdkgo.QueryDefinition {
	return ListTransactionsDefinition
}

// Invoke reads the provider resource and classifies the response.
func (operation ListTransactionsOperation) Invoke(call sdkgo.Call, input ListTransactionsInput) sdkgo.QueryAttempt[TransactionPage] {
	query := url.Values{}
	path := "/v1/reporting/transactions"
	start, startError := time.Parse(time.RFC3339, input.StartDate)
	end, endError := time.Parse(time.RFC3339, input.EndDate)
	if startError != nil || endError != nil || start.After(end) || end.Sub(start) > 31*24*time.Hour || input.Page < 0 {
		return failedAttempt[TransactionPage]("listTransactions", ListTransactionsBranchDefect, sdkgo.FailureValidation, "startDate and endDate must be RFC 3339, ordered within 31 days, and page nonnegative")
	}
	query.Set("start_date", input.StartDate)
	query.Set("end_date", input.EndDate)
	query.Set("fields", "transaction_info")
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	return executeRead(operation.client, call, "listTransactions", path, query, ListTransactionsBranchListed, ListTransactionsBranchNotFound, ListTransactionsBranchProviderRejected, ListTransactionsBranchInvalidResponse, ListTransactionsBranchDefect, decodeListTransactionsResponse)
}

func decodeListTransactionsResponse(body []byte) (TransactionPage, error) {
	var response TransactionPage
	err := json.Unmarshal(body, &response)
	if err != nil {
		return TransactionPage{}, err
	}
	return response, nil
}

type authResult struct {
	failure    *sdkgo.Failure
	branch     sdkgo.BranchID
	retry      bool
	retryAfter time.Duration
}

func (client *Client) authorize(call sdkgo.Call, operation string, request *http.Request, defectBranch, rejectedBranch, invalidBranch sdkgo.BranchID) authResult {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.ClientID) || !providerhttp.IsHeaderSafeCredential(credentials.ClientSecret.Reveal()) {
		return authResult{failure: failureFor(operation, sdkgo.FailureAuthentication, "PayPal credentials are unavailable"), branch: defectBranch}
	}
	tokenRequest, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+"/v1/oauth2/token", strings.NewReader("grant_type=client_credentials"))
	if err != nil {
		return authResult{failure: failureFor(operation, sdkgo.FailureLocalDefect, "PayPal token request could not be built"), branch: defectBranch}
	}
	tokenRequest.SetBasicAuth(credentials.ClientID, credentials.ClientSecret.Reveal())
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenResponse, err := client.httpClient.Do(tokenRequest)
	if err != nil {
		return authResult{failure: failureFor(operation, sdkgo.FailureAvailability, "PayPal token exchange failed"), retry: true}
	}
	defer func() { _ = tokenResponse.Body.Close() }()
	if tokenResponse.StatusCode == http.StatusTooManyRequests || tokenResponse.StatusCode == http.StatusRequestTimeout || tokenResponse.StatusCode >= 500 {
		return authResult{failure: failureFor(operation, sdkgo.FailureAvailability, "PayPal token exchange unavailable"), retry: true, retryAfter: providerhttp.ParseRetryAfter(tokenResponse.Header.Get("Retry-After"), time.Now())}
	}
	if tokenResponse.StatusCode != http.StatusOK {
		return authResult{failure: failureFor(operation, sdkgo.FailureAuthentication, "PayPal rejected client credentials"), branch: rejectedBranch}
	}
	body, err := providerhttp.ReadBoundedBody(tokenResponse.Body, 65536)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return authResult{failure: failureFor(operation, sdkgo.FailureResponseTooLarge, "PayPal token response exceeded the limit"), branch: invalidBranch}
	}
	if err != nil {
		return authResult{failure: failureFor(operation, sdkgo.FailureAvailability, "PayPal token response unavailable"), retry: true}
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if json.Unmarshal(body, &token) != nil || token.AccessToken == "" || !providerhttp.IsHeaderSafeCredential(token.AccessToken) || !strings.EqualFold(token.TokenType, "Bearer") {
		return authResult{failure: failureFor(operation, sdkgo.FailureProtocol, "PayPal returned an invalid token response"), branch: invalidBranch}
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
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
