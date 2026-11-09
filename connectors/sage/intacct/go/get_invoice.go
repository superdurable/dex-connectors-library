// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sageintacct

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

var invoiceKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// GetInvoiceInput identifies an accounts receivable invoice by its system-assigned key.
type GetInvoiceInput struct {
	// InvoiceKey is the key returned by the Sage Intacct invoice REST endpoint.
	InvoiceKey string `json:"invoiceKey"`
}

// InvoiceSummary contains the stable system key and displayed invoice ID.
type InvoiceSummary struct {
	// Key is the system-assigned invoice key.
	Key string `json:"key"`
	// ID is the invoice's displayed document ID.
	ID string `json:"id"`
}

// GetInvoiceOperation reads an invoice without mutating the provider.
type GetInvoiceOperation struct{ client *Client }

// GetInvoice binds the Query to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetInvoiceOperation) Definition() sdkgo.QueryDefinition { return GetInvoiceDefinition }

// Invoke classifies token, transport, HTTP, and invoice response outcomes.
func (operation GetInvoiceOperation) Invoke(call sdkgo.Call, input GetInvoiceInput) sdkgo.QueryAttempt[InvoiceSummary] {
	if !invoiceKeyPattern.MatchString(input.InvoiceKey) {
		return invoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureValidation, "invoice key is invalid")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return invoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureAuthentication, "Sage Intacct connection needs authorization")
		}
		return invoiceRetry(sdkgo.FailureTransport, "Sage Intacct credential refresh failed", 0)
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return invoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureAuthentication, "Sage Intacct access token is unavailable")
	}
	endpoint := operation.client.baseURL + "/ia/api/v1/objects/accounts-receivable/invoice/" + url.PathEscape(input.InvoiceKey)
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return invoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureValidation, "invoice request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return invoiceRetry(sdkgo.FailureTransport, "Sage Intacct invoice request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return invoiceBranch(GetInvoiceBranchDefect, sdkgo.FailureAuthentication, "Sage Intacct response contained a credential")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return invoiceRetry(sdkgo.FailureRateLimit, "Sage Intacct rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return invoiceRetry(sdkgo.FailureAvailability, "Sage Intacct is temporarily unavailable", retryAfter)
	}
	if response.StatusCode == http.StatusNotFound {
		return invoiceBranch(GetInvoiceBranchNotFound, sdkgo.FailureNotFound, "Sage Intacct invoice was not found")
	}
	if response.StatusCode != http.StatusOK {
		return invoiceBranch(GetInvoiceBranchProviderRejected, sdkgo.FailureProviderRejection, "Sage Intacct rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return invoiceBranch(GetInvoiceBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Sage Intacct invoice response exceeded the configured limit")
	}
	if readErr != nil {
		return invoiceRetry(sdkgo.FailureTransport, "Sage Intacct invoice response was interrupted", 0)
	}
	invoice, err := decodeInvoice(body, input.InvoiceKey)
	if err != nil {
		return invoiceBranch(GetInvoiceBranchInvalidResponse, sdkgo.FailureProtocol, "Sage Intacct returned invalid invoice JSON")
	}
	return sdkgo.NewQueryBranch(GetInvoiceBranchFound, invoice, nil, sdkgo.Receipt{})
}

func decodeInvoice(body []byte, requestedKey string) (InvoiceSummary, error) {
	var envelope struct {
		Result *InvoiceSummary `json:"ia::result"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return InvoiceSummary{}, err
	}
	if envelope.Result == nil || envelope.Result.Key != requestedKey || envelope.Result.ID == "" {
		return InvoiceSummary{}, errors.New("invoice result is incomplete or has a different key")
	}
	return *envelope.Result, nil
}

func invoiceFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "sage-intacct", Operation: "getInvoice", Message: message}
}

func invoiceBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[InvoiceSummary] {
	failure := invoiceFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, InvoiceSummary{}, &failure, sdkgo.Receipt{})
}

func invoiceRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[InvoiceSummary] {
	return sdkgo.NewQueryRetry[InvoiceSummary](invoiceFailure(kind, message), delay)
}
