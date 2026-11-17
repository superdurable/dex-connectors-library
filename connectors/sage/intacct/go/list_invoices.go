// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sageintacct

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListInvoicesInput requests the first page of Accounts Receivable invoices.
type ListInvoicesInput struct{}

// InvoiceList contains the keys and displayed IDs in one provider page.
type InvoiceList struct {
	// Invoices contains the invoice identities exposed by Sage Intacct.
	Invoices []InvoiceSummary `json:"invoices"`
	// Next is Sage Intacct's opaque link to the next page, or blank at the end.
	Next string `json:"next"`
}

// ListInvoicesOperation reads Accounts Receivable invoice identities.
type ListInvoicesOperation struct{ client *Client }

// ListInvoices binds the Query to this client.
func (client *Client) ListInvoices() ListInvoicesOperation { return ListInvoicesOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListInvoicesOperation) Definition() sdkgo.QueryDefinition { return ListInvoicesDefinition }

// Invoke reads one provider page without following an untrusted link.
func (operation ListInvoicesOperation) Invoke(call sdkgo.Call, _ ListInvoicesInput) sdkgo.QueryAttempt[InvoiceList] {
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return listInvoicesBranch(ListInvoicesBranchDefect, sdkgo.FailureAuthentication, "Sage Intacct connection needs authorization")
		}
		return listInvoicesRetry(sdkgo.FailureTransport, "Sage Intacct credential refresh failed", 0)
	}
	accessToken := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(accessToken) {
		return listInvoicesBranch(ListInvoicesBranchDefect, sdkgo.FailureAuthentication, "Sage Intacct access token is unavailable")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/ia/api/v1/objects/accounts-receivable/invoice", nil)
	if err != nil {
		return listInvoicesBranch(ListInvoicesBranchDefect, sdkgo.FailureValidation, "invoice list request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return listInvoicesRetry(sdkgo.FailureTransport, "Sage Intacct invoice list request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if bytes.Contains(body, []byte(accessToken)) {
		return listInvoicesBranch(ListInvoicesBranchDefect, sdkgo.FailureAuthentication, "Sage Intacct response contained a credential")
	}
	if response.StatusCode == http.StatusTooManyRequests {
		return listInvoicesRetry(sdkgo.FailureRateLimit, "Sage Intacct rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return listInvoicesRetry(sdkgo.FailureAvailability, "Sage Intacct is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return listInvoicesBranch(ListInvoicesBranchProviderRejected, sdkgo.FailureProviderRejection, "Sage Intacct rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return listInvoicesBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Sage Intacct invoice list response exceeded the configured limit")
	}
	if readErr != nil {
		return listInvoicesRetry(sdkgo.FailureTransport, "Sage Intacct invoice list response was interrupted", 0)
	}
	var envelope struct {
		Result []InvoiceSummary `json:"ia::result"`
		Meta   struct {
			Next string `json:"next"`
		} `json:"ia::meta"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Result == nil {
		return listInvoicesBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureProtocol, "Sage Intacct returned invalid invoice list JSON")
	}
	for _, invoice := range envelope.Result {
		if !invoiceKeyPattern.MatchString(invoice.Key) || invoice.ID == "" {
			return listInvoicesBranch(ListInvoicesBranchInvalidResponse, sdkgo.FailureProtocol, "Sage Intacct returned an incomplete invoice identity")
		}
	}
	return sdkgo.NewQueryBranch(ListInvoicesBranchFound, InvoiceList{Invoices: envelope.Result, Next: envelope.Meta.Next}, nil, sdkgo.Receipt{})
}

func listInvoicesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "sage-intacct", Operation: "listInvoices", Message: message}
}

func listInvoicesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[InvoiceList] {
	failure := listInvoicesFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, InvoiceList{}, &failure, sdkgo.Receipt{})
}

func listInvoicesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[InvoiceList] {
	return sdkgo.NewQueryRetry[InvoiceList](listInvoicesFailure(kind, message), delay)
}
