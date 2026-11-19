// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opensearch

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetDocumentInput identifies an index document by its stable ID.
type GetDocumentInput struct {
	// Index is the lowercase concrete index name.
	Index string `json:"index"`
	// DocumentID is the stable OpenSearch document ID.
	DocumentID string `json:"documentId"`
}

// RetrievedDocument is one indexed source and its version.
type RetrievedDocument struct {
	// Index is the index containing the source.
	Index string `json:"index"`
	// ID is the provider document ID.
	ID string `json:"id"`
	// Source is untrusted provider JSON and can contain sensitive user data.
	Source json.RawMessage `json:"source"`
	// Version is the OpenSearch document version.
	Version int64 `json:"version"`
}

// GetDocumentOperation retrieves one document by ID.
type GetDocumentOperation struct{ client *Client }

// GetDocument returns the typed document retrieval Query.
func (client *Client) GetDocument() GetDocumentOperation { return GetDocumentOperation{client} }

// Definition returns the manifest-generated retrieval contract.
func (GetDocumentOperation) Definition() sdkgo.QueryDefinition { return GetDocumentDefinition }

// Invoke retrieves the latest source and classifies missing documents separately from missing indexes.
func (operation GetDocumentOperation) Invoke(call sdkgo.Call, input GetDocumentInput) sdkgo.QueryAttempt[RetrievedDocument] {
	if !validIndexName(input.Index) || !validDocumentID(input.DocumentID) {
		return getDocumentFailure(GetDocumentBranchDefect, sdkgo.FailureValidation, "index or document ID is invalid")
	}
	exchange := exchangeDocument(operation.client, call, http.MethodGet, input.Index, input.DocumentID, nil)
	if exchange.requestFailure != nil {
		return getDocumentFailure(GetDocumentBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		return sdkgo.NewQueryRetry[RetrievedDocument](sdkgo.Failure{Kind: exchange.retryKind, Provider: "opensearch", Operation: "getDocument", Message: exchange.retryMessage}, exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		var missing struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
			Found *bool `json:"found"`
		}
		if json.Unmarshal(exchange.body, &missing) != nil {
			return getDocumentFailure(GetDocumentBranchInvalidResponse, sdkgo.FailureProtocol, "OpenSearch returned invalid missing-document data")
		}
		if missing.Error.Type == "index_not_found_exception" {
			return getDocumentFailure(GetDocumentBranchIndexNotFound, sdkgo.FailureNotFound, "OpenSearch index was not found")
		}
		if missing.Found != nil && !*missing.Found {
			return getDocumentFailure(GetDocumentBranchNotFound, sdkgo.FailureNotFound, "OpenSearch document was not found")
		}
		return getDocumentFailure(GetDocumentBranchInvalidResponse, sdkgo.FailureProtocol, "OpenSearch returned an unrecognized missing-document response")
	}
	if exchange.status != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if exchange.status == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if exchange.status == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return getDocumentFailure(GetDocumentBranchProviderRejected, kind, "OpenSearch rejected retrieval with HTTP "+strconv.Itoa(exchange.status))
	}
	var wire struct {
		Index   string          `json:"_index"`
		ID      string          `json:"_id"`
		Version int64           `json:"_version"`
		Found   *bool           `json:"found"`
		Source  json.RawMessage `json:"_source"`
	}
	if json.Unmarshal(exchange.body, &wire) != nil || wire.Index != input.Index || wire.ID != input.DocumentID || wire.Version <= 0 || wire.Found == nil || !*wire.Found || !validDocumentSource(wire.Source) {
		return getDocumentFailure(GetDocumentBranchInvalidResponse, sdkgo.FailureProtocol, "OpenSearch returned invalid document data")
	}
	return sdkgo.NewQueryBranch(GetDocumentBranchFound, RetrievedDocument{Index: wire.Index, ID: wire.ID, Source: wire.Source, Version: wire.Version}, nil, sdkgo.Receipt{ObservedAt: time.Now().UTC()})
}

func getDocumentFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[RetrievedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "opensearch", Operation: "getDocument", Message: message}
	return sdkgo.NewQueryBranch(branch, RetrievedDocument{}, &failure, sdkgo.Receipt{})
}
