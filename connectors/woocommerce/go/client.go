// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package woocommerce provides read-only WooCommerce Query operations for Dex Flows.
package woocommerce

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

// New validates the connection settings without contacting WooCommerce.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("WooCommerce credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("timeout and maxResponseBytes must be positive")
	}
	storeURL, err := providerhttp.ValidateBaseURL(config.StoreURL)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(strings.ToLower(storeURL), "/wp-json/wc/v3") {
		return nil, fmt.Errorf("storeUrl must be the WordPress Site Address without /wp-json/wc/v3")
	}
	baseURL := storeURL + "/wp-json/wc/v3"
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

// ListOrdersInput selects one WooCommerce order page.
type ListOrdersInput struct {
	// Page is one-based; zero uses the provider default.
	Page int `json:"page,omitempty"`
	// PerPage bounds one page to 100; zero uses the provider default.
	PerPage int `json:"perPage,omitempty"`
}

// GetOrderInput identifies one WooCommerce order.
type GetOrderInput struct {
	// OrderID is the numeric WooCommerce order ID.
	OrderID int64 `json:"orderId"`
}

// Order is a minimal WooCommerce order summary without customer data.
type Order struct {
	// ID is the numeric WooCommerce order ID.
	ID int64 `json:"id"`
	// Status is the provider order status.
	Status string `json:"status"`
	// Total is the decimal order total.
	Total string `json:"total"`
	// Currency is the ISO currency code.
	Currency string `json:"currency"`
}

// OrderPage contains one WooCommerce order page.
type OrderPage struct {
	// Orders contains the returned orders, possibly none.
	Orders []Order `json:"orders"`
}

// ListOrdersOperation executes the listOrders read.
type ListOrdersOperation struct{ client *Client }

// ListOrders returns the listOrders Query bound to this client.
func (client *Client) ListOrders() ListOrdersOperation { return ListOrdersOperation{client: client} }

// Definition returns the manifest-generated listOrders contract.
func (ListOrdersOperation) Definition() sdkgo.QueryDefinition { return ListOrdersDefinition }

// Invoke reads the provider resource and classifies the response.
func (operation ListOrdersOperation) Invoke(call sdkgo.Call, input ListOrdersInput) sdkgo.QueryAttempt[OrderPage] {
	query := url.Values{}
	path := "/orders"
	if input.Page < 0 || input.PerPage < 0 || input.PerPage > 100 {
		return failedAttempt[OrderPage]("listOrders", ListOrdersBranchDefect, sdkgo.FailureValidation, "page must be positive and perPage at most 100")
	}
	if input.Page > 0 {
		query.Set("page", strconv.Itoa(input.Page))
	}
	if input.PerPage > 0 {
		query.Set("per_page", strconv.Itoa(input.PerPage))
	}
	return executeRead(operation.client, call, "listOrders", path, query, ListOrdersBranchListed, ListOrdersBranchNotFound, ListOrdersBranchProviderRejected, ListOrdersBranchInvalidResponse, ListOrdersBranchDefect, decodeListOrdersResponse)
}

func decodeListOrdersResponse(body []byte) (OrderPage, error) {
	var orders []Order
	err := json.Unmarshal(body, &orders)
	if err != nil || orders == nil {
		return OrderPage{}, fmt.Errorf("missing orders")
	}
	return OrderPage{Orders: orders}, nil
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
	if input.OrderID <= 0 {
		return failedAttempt[Order]("getOrder", GetOrderBranchDefect, sdkgo.FailureValidation, "orderId must be positive")
	}
	path := "/orders/" + strconv.FormatInt(input.OrderID, 10)
	return executeRead(operation.client, call, "getOrder", path, query, GetOrderBranchFound, GetOrderBranchNotFound, GetOrderBranchProviderRejected, GetOrderBranchInvalidResponse, GetOrderBranchDefect, decodeGetOrderResponse)
}

func decodeGetOrderResponse(body []byte) (Order, error) {
	var response Order
	err := json.Unmarshal(body, &response)
	if err != nil {
		return Order{}, err
	}
	if response.ID <= 0 {
		return Order{}, fmt.Errorf("missing order")
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
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.ConsumerKey.Reveal()) || !providerhttp.IsHeaderSafeCredential(credentials.ConsumerSecret.Reveal()) {
		return authResult{failure: failureFor(operation, sdkgo.FailureAuthentication, "WooCommerce credentials are unavailable"), branch: defectBranch}
	}
	request.SetBasicAuth(credentials.ConsumerKey.Reveal(), credentials.ConsumerSecret.Reveal())
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
