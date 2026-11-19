// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetDocumentInput identifies one document to read.
type GetDocumentInput struct {
	// Index is one concrete lowercase Elasticsearch index name.
	Index string `json:"index"`
	// DocumentID is the stable provider document ID.
	DocumentID string `json:"documentId"`
}

// RetrievedDocument contains the complete untrusted source of one document.
type RetrievedDocument struct {
	// Index is the provider index name.
	Index string `json:"index"`
	// ID is the provider document ID.
	ID string `json:"id"`
	// Source is the document JSON, which may contain sensitive user data.
	Source json.RawMessage `json:"source"`
}

// GetDocumentOperation reads a document by index and stable ID.
type GetDocumentOperation struct{ client *Client }

// GetDocument returns the typed read Query bound to this client.
func (client *Client) GetDocument() GetDocumentOperation { return GetDocumentOperation{client} }

// Definition returns the manifest-generated document read contract.
func (GetDocumentOperation) Definition() sdkgo.QueryDefinition { return GetDocumentDefinition }

// Invoke reads a document and validates the provider identity and source.
func (operation GetDocumentOperation) Invoke(call sdkgo.Call, input GetDocumentInput) sdkgo.QueryAttempt[RetrievedDocument] {
	if !validIndexName(input.Index) || !validDocumentID(input.DocumentID) {
		return getDocumentFailure(GetDocumentBranchDefect, sdkgo.FailureValidation, "index or documentId is invalid")
	}
	exchange := exchangeDocument(operation.client, call, http.MethodGet, input.Index, input.DocumentID, nil)
	if exchange.requestFailure != nil {
		return getDocumentFailure(GetDocumentBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		if exchange.retryKind == sdkgo.FailureResponseTooLarge {
			return getDocumentFailure(GetDocumentBranchInvalidResponse, exchange.retryKind, exchange.retryMessage)
		}
		return getDocumentRetry(exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	switch exchange.status {
	case http.StatusNotFound:
		return getDocumentFailure(GetDocumentBranchNotFound, sdkgo.FailureNotFound, "Elasticsearch document was not found")
	case http.StatusUnauthorized:
		return getDocumentFailure(GetDocumentBranchProviderRejected, sdkgo.FailureAuthentication, "Elasticsearch rejected the credentials")
	case http.StatusForbidden:
		return getDocumentFailure(GetDocumentBranchProviderRejected, sdkgo.FailureAuthorization, "Elasticsearch denied document access")
	case http.StatusOK:
		var wire struct {
			Index  string          `json:"_index"`
			ID     string          `json:"_id"`
			Found  *bool           `json:"found"`
			Source json.RawMessage `json:"_source"`
		}
		if err := json.Unmarshal(exchange.body, &wire); err != nil || wire.Index != input.Index || wire.ID != input.DocumentID || wire.Found == nil || !*wire.Found || !validDocumentSource(wire.Source) {
			return getDocumentFailure(GetDocumentBranchInvalidResponse, sdkgo.FailureProtocol, "Elasticsearch returned unusable document data")
		}
		return sdkgo.NewQueryBranch(GetDocumentBranchFound, RetrievedDocument{Index: wire.Index, ID: wire.ID, Source: wire.Source}, nil, sdkgo.Receipt{})
	default:
		return getDocumentFailure(GetDocumentBranchProviderRejected, sdkgo.FailureProviderRejection, "Elasticsearch rejected document read with HTTP "+strconv.Itoa(exchange.status))
	}
}

func getDocumentFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[RetrievedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "getDocument", Message: message}
	return sdkgo.NewQueryBranch(branch, RetrievedDocument{}, &failure, sdkgo.Receipt{})
}

func getDocumentRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[RetrievedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "getDocument", Message: message}
	return sdkgo.NewQueryRetry[RetrievedDocument](failure, retryAfter)
}
