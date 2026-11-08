// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pandadoc

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

// GetDocumentInput selects a document by its non-secret ID.
type GetDocumentInput struct {
	// DocumentID is the ID returned by PandaDoc when a document is created.
	DocumentID string `json:"documentId"`
}

// DocumentStatus excludes recipients, fields, tokens, and pricing.
type DocumentStatus struct {
	// ID is the PandaDoc document ID.
	ID string `json:"id"`
	// Name is the document title.
	Name string `json:"name"`
	// Status is a PandaDoc document state such as document.completed.
	Status string `json:"status"`
}

// GetDocumentOperation reads one document's identity and state.
type GetDocumentOperation struct{ client *Client }

// GetDocument binds the Query to this client.
func (client *Client) GetDocument() GetDocumentOperation { return GetDocumentOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetDocumentOperation) Definition() sdkgo.QueryDefinition { return GetDocumentDefinition }

// Invoke reads one document and classifies retryable and definitive outcomes.
func (operation GetDocumentOperation) Invoke(call sdkgo.Call, input GetDocumentInput) sdkgo.QueryAttempt[DocumentStatus] {
	if !validDocumentID(input.DocumentID) {
		return documentBranch(GetDocumentBranchDefect, sdkgo.FailureValidation, "document ID must be 1–256 printable characters")
	}
	response := operation.client.get(call, "/documents/"+url.PathEscape(input.DocumentID))
	if errors.Is(response.err, errCredential) {
		return documentBranch(GetDocumentBranchDefect, sdkgo.FailureAuthentication, "PandaDoc credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[DocumentStatus](documentFailure(sdkgo.FailureTransport, "PandaDoc request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[DocumentStatus](documentFailure(sdkgo.FailureRateLimit, "PandaDoc rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[DocumentStatus](documentFailure(sdkgo.FailureAvailability, "PandaDoc is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return documentBranch(GetDocumentBranchNotFound, sdkgo.FailureNotFound, "PandaDoc did not expose the document")
	}
	if response.status != http.StatusOK {
		return documentBranch(GetDocumentBranchProviderRejected, sdkgo.FailureProviderRejection, "PandaDoc rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return documentBranch(GetDocumentBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "PandaDoc response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[DocumentStatus](documentFailure(sdkgo.FailureTransport, "PandaDoc response was interrupted"), 0)
	}
	document, err := decodeDocument(response.body, input.DocumentID)
	if err != nil {
		return documentBranch(GetDocumentBranchInvalidResponse, sdkgo.FailureProtocol, "PandaDoc returned invalid document JSON")
	}
	return sdkgo.NewQueryBranch(GetDocumentBranchFound, document, nil, sdkgo.Receipt{})
}

func validDocumentID(documentID string) bool {
	if len(documentID) == 0 || len(documentID) > 256 || !utf8.ValidString(documentID) {
		return false
	}
	for _, character := range documentID {
		if unicode.IsControl(character) || character == '/' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func decodeDocument(body []byte, requestedDocumentID string) (DocumentStatus, error) {
	var wire struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return DocumentStatus{}, err
	}
	if wire.ID != requestedDocumentID || strings.TrimSpace(wire.Name) == "" || !strings.HasPrefix(wire.Status, "document.") || len(wire.Status) <= len("document.") {
		return DocumentStatus{}, fmt.Errorf("document identity or status missing")
	}
	return DocumentStatus{ID: wire.ID, Name: wire.Name, Status: wire.Status}, nil
}

func documentFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "pandadoc", Operation: "getDocument", Message: message}
}

func documentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DocumentStatus] {
	failure := documentFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, DocumentStatus{}, &failure, sdkgo.Receipt{})
}
