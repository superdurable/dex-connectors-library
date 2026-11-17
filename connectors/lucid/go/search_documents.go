// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lucid

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchDocumentsInput selects one keyword-matching document page.
type SearchDocumentsInput struct {
	// Keywords matches document titles and content; it must contain 1 to 400 characters.
	Keywords string `json:"keywords"`
	// PageToken is blank for the first page or a previous NextPageToken value.
	PageToken string `json:"pageToken,omitempty"`
}

// SearchDocumentsOperation searches accessible non-trashed documents.
type SearchDocumentsOperation struct{ client *Client }

// SearchDocuments binds the Query to this client.
func (client *Client) SearchDocuments() SearchDocumentsOperation {
	return SearchDocumentsOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (SearchDocumentsOperation) Definition() sdkgo.QueryDefinition { return SearchDocumentsDefinition }

// Invoke returns one page of matching document identities and titles.
func (operation SearchDocumentsOperation) Invoke(call sdkgo.Call, input SearchDocumentsInput) sdkgo.QueryAttempt[DocumentsPage] {
	keywords := strings.TrimSpace(input.Keywords)
	if keywords == "" || utf8.RuneCountInString(keywords) > 400 || !validPageToken(input.PageToken) {
		return searchDocumentsBranch(SearchDocumentsBranchDefect, sdkgo.FailureValidation, "keywords or page token are invalid")
	}
	body, err := json.Marshal(struct {
		ExcludeTrashed bool   `json:"excludeTrashed"`
		Keywords       string `json:"keywords"`
	}{ExcludeTrashed: true, Keywords: keywords})
	if err != nil {
		return searchDocumentsBranch(SearchDocumentsBranchDefect, sdkgo.FailureLocalDefect, "document search encoding failed")
	}
	query := url.Values{"pageSize": {"200"}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.searchWithBody(call, "/documents/search?"+query.Encode(), body)
	if errors.Is(response.err, errCredential) {
		return searchDocumentsBranch(SearchDocumentsBranchDefect, sdkgo.FailureAuthentication, "Lucid credentials unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return searchDocumentsRetry(sdkgo.FailureTransport, "Lucid request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return searchDocumentsRetry(sdkgo.FailureRateLimit, "Lucid rate limited the search", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return searchDocumentsRetry(sdkgo.FailureAvailability, "Lucid is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return searchDocumentsBranch(SearchDocumentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Lucid rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return searchDocumentsBranch(SearchDocumentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lucid response exceeded the configured limit")
	}
	if response.err != nil {
		return searchDocumentsRetry(sdkgo.FailureTransport, "Lucid response was interrupted", 0)
	}
	page, err := decodeDocumentsPage(response.body, response.link)
	if err != nil {
		return searchDocumentsBranch(SearchDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "Lucid returned invalid document search JSON or pagination")
	}
	return sdkgo.NewQueryBranch(SearchDocumentsBranchListed, page, nil, sdkgo.Receipt{})
}

func searchDocumentsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DocumentsPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lucid", Operation: "searchDocuments", Message: message}
	return sdkgo.NewQueryBranch(branch, DocumentsPage{}, &failure, sdkgo.Receipt{})
}

func searchDocumentsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[DocumentsPage] {
	failure := sdkgo.Failure{Kind: kind, Provider: "lucid", Operation: "searchDocuments", Message: message}
	return sdkgo.NewQueryRetry[DocumentsPage](failure, delay)
}
