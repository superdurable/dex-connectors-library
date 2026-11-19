// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lucid

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetDocumentInput identifies one Lucid document by UUID.
type GetDocumentInput struct {
	// ID is the document ID returned by listDocuments.
	ID string `json:"id"`
}

// DocumentDetails contains selected non-content metadata for one document.
type DocumentDetails struct {
	// ID is the Lucid document UUID.
	ID string `json:"id"`
	// Title is the document title.
	Title string `json:"title"`
	// Product is lucidchart, lucidscale, or lucidspark.
	Product string `json:"product"`
	// Version is the document's latest version number.
	Version int64 `json:"version"`
	// PageCount is the number of document pages.
	PageCount int64 `json:"pageCount"`
	// CanEdit reports whether the requesting key can edit this document.
	CanEdit bool `json:"canEdit"`
}

// GetDocumentOperation reads one document's metadata.
type GetDocumentOperation struct{ client *Client }

// GetDocument binds the document Query to this client.
func (client *Client) GetDocument() GetDocumentOperation { return GetDocumentOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetDocumentOperation) Definition() sdkgo.QueryDefinition { return GetDocumentDefinition }

var documentIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Invoke reads selected metadata and classifies retryable and definitive outcomes.
func (operation GetDocumentOperation) Invoke(call sdkgo.Call, input GetDocumentInput) sdkgo.QueryAttempt[DocumentDetails] {
	if !documentIDPattern.MatchString(input.ID) {
		return documentDetailsBranch(GetDocumentBranchDefect, sdkgo.FailureValidation, "document ID must be a UUID")
	}
	response := operation.client.get(call, "/documents/"+input.ID)
	if errors.Is(response.err, errCredential) {
		return documentDetailsBranch(GetDocumentBranchDefect, sdkgo.FailureAuthentication, "Lucid credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[DocumentDetails](documentDetailsFailure(sdkgo.FailureTransport, "Lucid request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[DocumentDetails](documentDetailsFailure(sdkgo.FailureRateLimit, "Lucid rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[DocumentDetails](documentDetailsFailure(sdkgo.FailureAvailability, "Lucid is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return documentDetailsBranch(GetDocumentBranchNotFound, sdkgo.FailureProviderRejection, "Lucid reported the document missing")
	}
	if response.status != http.StatusOK {
		return documentDetailsBranch(GetDocumentBranchProviderRejected, sdkgo.FailureProviderRejection, "Lucid rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return documentDetailsBranch(GetDocumentBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lucid response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[DocumentDetails](documentDetailsFailure(sdkgo.FailureTransport, "Lucid response was interrupted"), 0)
	}
	var wire struct {
		DocumentID string `json:"documentId"`
		Title      string `json:"title"`
		Product    string `json:"product"`
		Version    int64  `json:"version"`
		PageCount  int64  `json:"pageCount"`
		CanEdit    bool   `json:"canEdit"`
	}
	if err := json.Unmarshal(response.body, &wire); err != nil || !strings.EqualFold(wire.DocumentID, input.ID) || strings.TrimSpace(wire.Title) == "" || wire.Version < 0 || wire.PageCount < 0 || !isLucidProduct(wire.Product) {
		return documentDetailsBranch(GetDocumentBranchInvalidResponse, sdkgo.FailureProtocol, "Lucid returned invalid document JSON")
	}
	return sdkgo.NewQueryBranch(GetDocumentBranchFound, DocumentDetails{ID: wire.DocumentID, Title: wire.Title, Product: wire.Product, Version: wire.Version, PageCount: wire.PageCount, CanEdit: wire.CanEdit}, nil, sdkgo.Receipt{})
}

func isLucidProduct(product string) bool {
	return product == "lucidchart" || product == "lucidscale" || product == "lucidspark"
}

func documentDetailsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lucid", Operation: "getDocument", Message: message}
}

func documentDetailsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DocumentDetails] {
	failure := documentDetailsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, DocumentDetails{}, &failure, sdkgo.Receipt{})
}
