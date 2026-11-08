// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

import (
	"bytes"
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

// IndexDocumentInput fully replaces one document under a stable caller-selected ID.
type IndexDocumentInput struct {
	// Index is one concrete lowercase Elasticsearch index name.
	Index string `json:"index"`
	// DocumentID is the stable ID used for every retry.
	DocumentID string `json:"documentId"`
	// Source is a complete JSON object replacing the current document.
	Source json.RawMessage `json:"source"`
}

// IndexedDocument identifies the document stored by indexDocument.
type IndexedDocument struct {
	// Index is the index that contains the document.
	Index string `json:"index"`
	// ID is the stable document ID.
	ID string `json:"id"`
}

// DeleteDocumentInput identifies one document to remove.
type DeleteDocumentInput struct {
	// Index is one concrete lowercase Elasticsearch index name.
	Index string `json:"index"`
	// DocumentID is the stable ID to remove.
	DocumentID string `json:"documentId"`
}

// DeletedDocument reports that the target document is absent.
type DeletedDocument struct {
	// Index is the index containing the target ID.
	Index string `json:"index"`
	// ID is the stable target document ID.
	ID string `json:"id"`
	// WasDeleted reports whether this attempt removed an existing document.
	WasDeleted bool `json:"wasDeleted"`
}

// IndexDocumentOperation replaces one document with an absolute source JSON object.
type IndexDocumentOperation struct{ client *Client }

// DeleteDocumentOperation ensures one document ID is absent.
type DeleteDocumentOperation struct{ client *Client }

// Definition returns the manifest-generated indexing contract.
func (IndexDocumentOperation) Definition() sdkgo.MutationDefinition { return IndexDocumentDefinition }

// Definition returns the manifest-generated deletion contract.
func (DeleteDocumentOperation) Definition() sdkgo.MutationDefinition { return DeleteDocumentDefinition }

// IdempotencyKey records the stable call identity; absolute replacement makes repeating the PUT safe.
func (IndexDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ IndexDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the stable call identity; deleting the same ID again is safe.
func (DeleteDocumentOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteDocumentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke fully replaces a document. A lost or unusable response retries the same absolute PUT.
func (operation IndexDocumentOperation) Invoke(call sdkgo.Call, input IndexDocumentInput) sdkgo.MutationAttempt[IndexedDocument] {
	if !validIndexName(input.Index) || !validDocumentID(input.DocumentID) || len(input.Source) == 0 || int64(len(input.Source)) > operation.client.maxDocumentBytes || !validDocumentSource(input.Source) {
		return indexFailure(IndexDocumentBranchDefect, sdkgo.FailureValidation, "index, documentId, or source is invalid")
	}
	exchange := exchangeDocument(operation.client, call, http.MethodPut, input.Index, input.DocumentID, input.Source)
	if exchange.requestFailure != nil {
		return indexFailure(IndexDocumentBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		return indexRetry(exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	switch exchange.status {
	case http.StatusOK, http.StatusCreated:
		var wire struct {
			Index  string `json:"_index"`
			ID     string `json:"_id"`
			Result string `json:"result"`
		}
		if err := json.Unmarshal(exchange.body, &wire); err != nil || wire.Index != input.Index || wire.ID != input.DocumentID || (wire.Result != "created" && wire.Result != "updated") {
			return indexRetry(sdkgo.FailureProtocol, "Elasticsearch indexing confirmation is unusable", 0)
		}
		return sdkgo.NewMutationBranch(IndexDocumentBranchIndexed, IndexedDocument{Index: input.Index, ID: input.DocumentID}, nil, sdkgo.Receipt{})
	case http.StatusUnauthorized:
		return indexFailure(IndexDocumentBranchProviderRejected, sdkgo.FailureAuthentication, "Elasticsearch rejected the credentials")
	case http.StatusForbidden:
		return indexFailure(IndexDocumentBranchProviderRejected, sdkgo.FailureAuthorization, "Elasticsearch denied index access")
	default:
		return indexFailure(IndexDocumentBranchProviderRejected, sdkgo.FailureProviderRejection, "Elasticsearch rejected indexing with HTTP "+strconv.Itoa(exchange.status))
	}
}

// Invoke deletes one stable ID. A repeated delete that finds no document still succeeds.
func (operation DeleteDocumentOperation) Invoke(call sdkgo.Call, input DeleteDocumentInput) sdkgo.MutationAttempt[DeletedDocument] {
	if !validIndexName(input.Index) || !validDocumentID(input.DocumentID) {
		return deleteFailure(DeleteDocumentBranchDefect, sdkgo.FailureValidation, "index or documentId is invalid")
	}
	exchange := exchangeDocument(operation.client, call, http.MethodDelete, input.Index, input.DocumentID, nil)
	if exchange.requestFailure != nil {
		return deleteFailure(DeleteDocumentBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		return deleteRetry(exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	switch exchange.status {
	case http.StatusOK, http.StatusNotFound:
		var wire struct {
			Index  string `json:"_index"`
			ID     string `json:"_id"`
			Result string `json:"result"`
			Error  struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal(exchange.body, &wire); err != nil {
			return deleteRetry(sdkgo.FailureProtocol, "Elasticsearch deletion confirmation is unusable", 0)
		}
		if exchange.status == http.StatusNotFound && wire.Error.Type == "index_not_found_exception" {
			return deleteFailure(DeleteDocumentBranchIndexNotFound, sdkgo.FailureNotFound, "Elasticsearch index was not found")
		}
		if wire.Index != input.Index || wire.ID != input.DocumentID || (wire.Result != "deleted" && wire.Result != "not_found") {
			return deleteRetry(sdkgo.FailureProtocol, "Elasticsearch deletion confirmation is incomplete", 0)
		}
		return sdkgo.NewMutationBranch(DeleteDocumentBranchAbsent, DeletedDocument{Index: input.Index, ID: input.DocumentID, WasDeleted: wire.Result == "deleted"}, nil, sdkgo.Receipt{})
	case http.StatusUnauthorized:
		return deleteFailure(DeleteDocumentBranchProviderRejected, sdkgo.FailureAuthentication, "Elasticsearch rejected the credentials")
	case http.StatusForbidden:
		return deleteFailure(DeleteDocumentBranchProviderRejected, sdkgo.FailureAuthorization, "Elasticsearch denied index access")
	default:
		return deleteFailure(DeleteDocumentBranchProviderRejected, sdkgo.FailureProviderRejection, "Elasticsearch rejected deletion with HTTP "+strconv.Itoa(exchange.status))
	}
}

type documentExchange struct {
	status         int
	body           []byte
	requestFailure *sdkgo.Failure
	shouldRetry    bool
	retryKind      sdkgo.FailureKind
	retryMessage   string
	retryAfter     time.Duration
}

func exchangeDocument(client *Client, call sdkgo.Call, method, index, documentID string, source []byte) documentExchange {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return documentExchange{shouldRetry: true, retryKind: sdkgo.FailureAuthentication, retryMessage: "Elasticsearch credentials are temporarily unavailable"}
	}
	if credentials.Validate() != nil || !validUsername(credentials.Username) || !providerhttp.IsHeaderSafeCredential(credentials.Password.Reveal()) {
		return documentExchange{requestFailure: &sdkgo.Failure{Kind: sdkgo.FailureAuthentication, Provider: "elasticsearch", Message: "Elasticsearch credentials are invalid"}}
	}
	endpoint := client.clusterURL + "/" + index + "/_doc/" + url.PathEscape(documentID)
	request, err := http.NewRequestWithContext(call.Context, method, endpoint, bytes.NewReader(source))
	if err != nil {
		return documentExchange{requestFailure: &sdkgo.Failure{Kind: sdkgo.FailureLocalDefect, Provider: "elasticsearch", Message: "Elasticsearch document request could not be built"}}
	}
	request.SetBasicAuth(credentials.Username, credentials.Password.Reveal())
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPut {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return documentExchange{shouldRetry: true, retryKind: sdkgo.FailureTransport, retryMessage: "Elasticsearch document request failed"}
	}
	// Absolute replacement and deletion make an interrupted response safe to retry.
	defer func() { _ = response.Body.Close() }()
	exchange := documentExchange{status: response.StatusCode}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError {
		exchange.shouldRetry, exchange.retryKind, exchange.retryMessage = true, sdkgo.FailureAvailability, "Elasticsearch document operation is temporarily unavailable"
		if response.StatusCode == http.StatusTooManyRequests {
			exchange.retryKind = sdkgo.FailureRateLimit
		}
		exchange.retryAfter = providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		return exchange
	}
	exchange.body, err = providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return documentExchange{shouldRetry: true, retryKind: sdkgo.FailureResponseTooLarge, retryMessage: "Elasticsearch document response exceeded the configured limit"}
	}
	if err != nil {
		return documentExchange{shouldRetry: true, retryKind: sdkgo.FailureTransport, retryMessage: "Elasticsearch document response was interrupted"}
	}
	return exchange
}

func validDocumentID(documentID string) bool {
	return documentID != "" && len(documentID) <= 512 && utf8.ValidString(documentID) && !strings.ContainsAny(documentID, "\r\n\x00")
}

func validDocumentSource(source json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(source, &object) == nil && object != nil
}

func indexFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[IndexedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "indexDocument", Message: message}
	return sdkgo.NewMutationBranch(branch, IndexedDocument{}, &failure, sdkgo.Receipt{})
}
func indexRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[IndexedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "indexDocument", Message: message}
	return sdkgo.NewMutationRetry[IndexedDocument](failure, retryAfter)
}
func deleteFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[DeletedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "deleteDocument", Message: message}
	return sdkgo.NewMutationBranch(branch, DeletedDocument{}, &failure, sdkgo.Receipt{})
}
func deleteRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[DeletedDocument] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: "deleteDocument", Message: message}
	return sdkgo.NewMutationRetry[DeletedDocument](failure, retryAfter)
}
