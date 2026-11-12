// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lucid

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListDocumentsInput selects a page by its opaque, non-secret token.
type ListDocumentsInput struct {
	// PageToken is blank for the first page or a previous NextPageToken value.
	PageToken string `json:"pageToken,omitempty"`
}

// DocumentMetadata contains only the document ID and title.
type DocumentMetadata struct {
	// ID is the Lucid document UUID.
	ID string `json:"id"`
	// Title is the document title.
	Title string `json:"title"`
}

// DocumentsPage contains one page and the next opaque page token, if any.
type DocumentsPage struct {
	// Documents excludes content, owners, URLs, and custom attributes.
	Documents []DocumentMetadata `json:"documents"`
	// NextPageToken is blank at the last page.
	NextPageToken string `json:"nextPageToken"`
}

// ListDocumentsOperation searches accessible non-trashed documents.
type ListDocumentsOperation struct{ client *Client }

// ListDocuments binds the Query to this client.
func (client *Client) ListDocuments() ListDocumentsOperation { return ListDocumentsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListDocumentsOperation) Definition() sdkgo.QueryDefinition { return ListDocumentsDefinition }

// Invoke reads one page and classifies retryable and definitive outcomes.
func (operation ListDocumentsOperation) Invoke(call sdkgo.Call, input ListDocumentsInput) sdkgo.QueryAttempt[DocumentsPage] {
	if !validPageToken(input.PageToken) {
		return documentsBranch(ListDocumentsBranchDefect, sdkgo.FailureValidation, "page token contains invalid characters or exceeds 2048 bytes")
	}
	query := url.Values{"pageSize": {"200"}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	response := operation.client.search(call, "/documents/search?"+query.Encode())
	if errors.Is(response.err, errCredential) {
		return documentsBranch(ListDocumentsBranchDefect, sdkgo.FailureAuthentication, "Lucid credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[DocumentsPage](documentsFailure(sdkgo.FailureTransport, "Lucid request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[DocumentsPage](documentsFailure(sdkgo.FailureRateLimit, "Lucid rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[DocumentsPage](documentsFailure(sdkgo.FailureAvailability, "Lucid is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return documentsBranch(ListDocumentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Lucid rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return documentsBranch(ListDocumentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Lucid response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[DocumentsPage](documentsFailure(sdkgo.FailureTransport, "Lucid response was interrupted"), 0)
	}
	page, err := decodeDocumentsPage(response.body, response.link)
	if err != nil {
		return documentsBranch(ListDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "Lucid returned invalid document JSON or pagination")
	}
	return sdkgo.NewQueryBranch(ListDocumentsBranchListed, page, nil, sdkgo.Receipt{})
}

func validPageToken(token string) bool {
	if len(token) > 2048 || !utf8.ValidString(token) {
		return false
	}
	for _, character := range token {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func decodeDocumentsPage(body []byte, link string) (DocumentsPage, error) {
	var wire []struct {
		DocumentID string `json:"documentId"`
		Title      string `json:"title"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DocumentsPage{}, err
	}
	if wire == nil {
		return DocumentsPage{}, fmt.Errorf("missing documents array")
	}
	page := DocumentsPage{Documents: make([]DocumentMetadata, 0, len(wire))}
	for _, document := range wire {
		if document.DocumentID == "" || strings.TrimSpace(document.Title) == "" {
			return DocumentsPage{}, fmt.Errorf("missing document identity or title")
		}
		page.Documents = append(page.Documents, DocumentMetadata{ID: document.DocumentID, Title: document.Title})
	}
	token, err := nextPageToken(link)
	if err != nil {
		return DocumentsPage{}, err
	}
	page.NextPageToken = token
	return page, nil
}

func nextPageToken(link string) (string, error) {
	if link == "" {
		return "", nil
	}
	for _, segment := range strings.Split(link, ",") {
		parts := strings.Split(strings.TrimSpace(segment), ";")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid Link header")
		}
		isNext := false
		for _, attribute := range parts[1:] {
			if strings.TrimSpace(attribute) == `rel="next"` || strings.TrimSpace(attribute) == "rel=next" {
				isNext = true
			}
		}
		if !isNext {
			continue
		}
		rawURL := strings.TrimSpace(parts[0])
		if !strings.HasPrefix(rawURL, "<") || !strings.HasSuffix(rawURL, ">") {
			return "", fmt.Errorf("invalid next URL")
		}
		nextURL, err := url.Parse(strings.TrimSuffix(strings.TrimPrefix(rawURL, "<"), ">"))
		if err != nil || nextURL.Scheme != "https" || nextURL.Host != "api.lucid.co" || (nextURL.Path != "/v1/documents/search" && nextURL.Path != "/documents/search") {
			return "", fmt.Errorf("invalid next URL")
		}
		tokens := nextURL.Query()["pageToken"]
		if len(tokens) != 1 || tokens[0] == "" || !validPageToken(tokens[0]) {
			return "", fmt.Errorf("invalid next page token")
		}
		return tokens[0], nil
	}
	return "", nil
}

func documentsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "lucid", Operation: "listDocuments", Message: message}
}

func documentsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DocumentsPage] {
	failure := documentsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, DocumentsPage{}, &failure, sdkgo.Receipt{})
}
