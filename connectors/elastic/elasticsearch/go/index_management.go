// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elasticsearch

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// IndexNameInput identifies one concrete index; wildcards and aliases are excluded.
type IndexNameInput struct {
	// Index is the exact lowercase index name.
	Index string `json:"index"`
}

// IndexResult identifies the index created or removed.
type IndexResult struct {
	// Index is the exact index name.
	Index string `json:"index"`
}

// ListIndicesInput requests metadata for accessible non-hidden indices.
type ListIndicesInput struct{}

// IndexDetails holds one index's provider metadata.
type IndexDetails struct {
	// Index is the exact provider index name.
	Index string `json:"index"`
	// Metadata contains settings, mappings, and aliases; applications must treat it as untrusted.
	Metadata json.RawMessage `json:"metadata"`
}

// IndexList contains the accessible index metadata.
type IndexList struct {
	// Indices is sorted by index name.
	Indices []IndexDetails `json:"indices"`
}

// CreateIndexOperation ensures an index exists.
type CreateIndexOperation struct{ client *Client }

// DeleteIndexOperation ensures an index is absent.
type DeleteIndexOperation struct{ client *Client }

// GetIndexOperation reads one index's metadata.
type GetIndexOperation struct{ client *Client }

// ListIndicesOperation reads accessible index metadata.
type ListIndicesOperation struct{ client *Client }

// CreateIndex returns the typed index creation Mutation.
func (client *Client) CreateIndex() CreateIndexOperation { return CreateIndexOperation{client} }

// DeleteIndex returns the typed index deletion Mutation.
func (client *Client) DeleteIndex() DeleteIndexOperation { return DeleteIndexOperation{client} }

// GetIndex returns the typed index metadata Query.
func (client *Client) GetIndex() GetIndexOperation { return GetIndexOperation{client} }

// ListIndices returns the typed index listing Query.
func (client *Client) ListIndices() ListIndicesOperation { return ListIndicesOperation{client} }

// Definition returns the manifest-generated index creation contract.
func (CreateIndexOperation) Definition() sdkgo.MutationDefinition { return CreateIndexDefinition }

// Definition returns the manifest-generated index deletion contract.
func (DeleteIndexOperation) Definition() sdkgo.MutationDefinition { return DeleteIndexDefinition }

// Definition returns the manifest-generated index read contract.
func (GetIndexOperation) Definition() sdkgo.QueryDefinition { return GetIndexDefinition }

// Definition returns the manifest-generated index listing contract.
func (ListIndicesOperation) Definition() sdkgo.QueryDefinition { return ListIndicesDefinition }

// IdempotencyKey records the call identity; index creation converges on existence.
func (CreateIndexOperation) IdempotencyKey(callID sdkgo.CallID, _ IndexNameInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey records the call identity; repeated deletion converges on absence.
func (DeleteIndexOperation) IdempotencyKey(callID sdkgo.CallID, _ IndexNameInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an index or confirms one already exists.
func (operation CreateIndexOperation) Invoke(call sdkgo.Call, input IndexNameInput) sdkgo.MutationAttempt[IndexResult] {
	if !validIndexName(input.Index) {
		return indexManagementMutationFailure("createIndex", CreateIndexBranchDefect, sdkgo.FailureValidation, "index name is invalid")
	}
	exchange := exchangeIndex(operation.client, call, http.MethodPut, "/"+input.Index, nil)
	if exchange.requestFailure != nil {
		return indexManagementMutationFailure("createIndex", CreateIndexBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		return indexManagementMutationRetry("createIndex", exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	if exchange.status == http.StatusOK {
		if isAcknowledged(exchange.body) {
			return sdkgo.NewMutationBranch(CreateIndexBranchPresent, IndexResult{Index: input.Index}, nil, sdkgo.Receipt{})
		}
		return indexManagementMutationRetry("createIndex", sdkgo.FailureProtocol, "Elasticsearch index creation was not confirmed", 0)
	}
	if exchange.status == http.StatusBadRequest && indexErrorType(exchange.body) == "resource_already_exists_exception" {
		return sdkgo.NewMutationBranch(CreateIndexBranchPresent, IndexResult{Index: input.Index}, nil, sdkgo.Receipt{})
	}
	return indexManagementMutationFailure("createIndex", CreateIndexBranchProviderRejected, indexStatusFailureKind(exchange.status), "Elasticsearch rejected index creation with HTTP "+strconv.Itoa(exchange.status))
}

// Invoke deletes a concrete index, accepting a repeated deletion.
func (operation DeleteIndexOperation) Invoke(call sdkgo.Call, input IndexNameInput) sdkgo.MutationAttempt[IndexResult] {
	if !validIndexName(input.Index) {
		return indexManagementMutationFailure("deleteIndex", DeleteIndexBranchDefect, sdkgo.FailureValidation, "index name is invalid")
	}
	exchange := exchangeIndex(operation.client, call, http.MethodDelete, "/"+input.Index, nil)
	if exchange.requestFailure != nil {
		return indexManagementMutationFailure("deleteIndex", DeleteIndexBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		return indexManagementMutationRetry("deleteIndex", exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	if exchange.status == http.StatusOK && isAcknowledged(exchange.body) || exchange.status == http.StatusNotFound && indexErrorType(exchange.body) == "index_not_found_exception" {
		return sdkgo.NewMutationBranch(DeleteIndexBranchAbsent, IndexResult{Index: input.Index}, nil, sdkgo.Receipt{})
	}
	if exchange.status == http.StatusOK {
		return indexManagementMutationRetry("deleteIndex", sdkgo.FailureProtocol, "Elasticsearch index deletion was not confirmed", 0)
	}
	return indexManagementMutationFailure("deleteIndex", DeleteIndexBranchProviderRejected, indexStatusFailureKind(exchange.status), "Elasticsearch rejected index deletion with HTTP "+strconv.Itoa(exchange.status))
}

// Invoke reads metadata for one concrete index.
func (operation GetIndexOperation) Invoke(call sdkgo.Call, input IndexNameInput) sdkgo.QueryAttempt[IndexDetails] {
	if !validIndexName(input.Index) {
		return indexManagementQueryFailure[IndexDetails]("getIndex", GetIndexBranchDefect, sdkgo.FailureValidation, "index name is invalid")
	}
	exchange := exchangeIndex(operation.client, call, http.MethodGet, "/"+input.Index, nil)
	if exchange.requestFailure != nil {
		return indexManagementQueryFailure[IndexDetails]("getIndex", GetIndexBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		if exchange.retryKind == sdkgo.FailureResponseTooLarge {
			return indexManagementQueryFailure[IndexDetails]("getIndex", GetIndexBranchInvalidResponse, exchange.retryKind, exchange.retryMessage)
		}
		return indexManagementQueryRetry[IndexDetails]("getIndex", exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	if exchange.status == http.StatusNotFound {
		return indexManagementQueryFailure[IndexDetails]("getIndex", GetIndexBranchNotFound, sdkgo.FailureNotFound, "Elasticsearch index was not found")
	}
	if exchange.status != http.StatusOK {
		return indexManagementQueryFailure[IndexDetails]("getIndex", GetIndexBranchProviderRejected, indexStatusFailureKind(exchange.status), "Elasticsearch rejected index read with HTTP "+strconv.Itoa(exchange.status))
	}
	indices, ok := decodeIndexMetadata(exchange.body)
	if !ok || len(indices) != 1 || indices[0].Index != input.Index {
		return indexManagementQueryFailure[IndexDetails]("getIndex", GetIndexBranchInvalidResponse, sdkgo.FailureProtocol, "Elasticsearch returned unusable index metadata")
	}
	return sdkgo.NewQueryBranch(GetIndexBranchFound, indices[0], nil, sdkgo.Receipt{})
}

// Invoke lists accessible indices through the application-oriented index API.
func (operation ListIndicesOperation) Invoke(call sdkgo.Call, _ ListIndicesInput) sdkgo.QueryAttempt[IndexList] {
	exchange := exchangeIndex(operation.client, call, http.MethodGet, "/*?expand_wildcards=open,closed&allow_no_indices=true", nil)
	if exchange.requestFailure != nil {
		return indexManagementQueryFailure[IndexList]("listIndices", ListIndicesBranchDefect, exchange.requestFailure.Kind, exchange.requestFailure.Message)
	}
	if exchange.shouldRetry {
		if exchange.retryKind == sdkgo.FailureResponseTooLarge {
			return indexManagementQueryFailure[IndexList]("listIndices", ListIndicesBranchInvalidResponse, exchange.retryKind, exchange.retryMessage)
		}
		return indexManagementQueryRetry[IndexList]("listIndices", exchange.retryKind, exchange.retryMessage, exchange.retryAfter)
	}
	if exchange.status != http.StatusOK {
		return indexManagementQueryFailure[IndexList]("listIndices", ListIndicesBranchProviderRejected, indexStatusFailureKind(exchange.status), "Elasticsearch rejected index listing with HTTP "+strconv.Itoa(exchange.status))
	}
	indices, ok := decodeIndexMetadata(exchange.body)
	if !ok {
		return indexManagementQueryFailure[IndexList]("listIndices", ListIndicesBranchInvalidResponse, sdkgo.FailureProtocol, "Elasticsearch returned unusable index metadata")
	}
	return sdkgo.NewQueryBranch(ListIndicesBranchFound, IndexList{Indices: indices}, nil, sdkgo.Receipt{})
}

func exchangeIndex(client *Client, call sdkgo.Call, method, path string, body []byte) documentExchange {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !validUsername(credentials.Username) || !providerhttp.IsHeaderSafeCredential(credentials.Password.Reveal()) {
		return documentExchange{requestFailure: &sdkgo.Failure{Kind: sdkgo.FailureAuthentication, Provider: "elasticsearch", Message: "Elasticsearch credentials are unavailable or invalid"}}
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.clusterURL+path, bytes.NewReader(body))
	if err != nil {
		return documentExchange{requestFailure: &sdkgo.Failure{Kind: sdkgo.FailureLocalDefect, Provider: "elasticsearch", Message: "index request could not be built"}}
	}
	request.SetBasicAuth(credentials.Username, credentials.Password.Reveal())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return documentExchange{shouldRetry: true, retryKind: sdkgo.FailureTransport, retryMessage: "Elasticsearch index request failed"}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable || response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		kind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return documentExchange{shouldRetry: true, retryKind: kind, retryMessage: "Elasticsearch index operation is temporarily unavailable", retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())}
	}
	responseBody, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return documentExchange{shouldRetry: true, retryKind: sdkgo.FailureResponseTooLarge, retryMessage: "Elasticsearch index response exceeded the configured limit"}
	}
	if err != nil {
		return documentExchange{shouldRetry: true, retryKind: sdkgo.FailureTransport, retryMessage: "Elasticsearch index response was interrupted"}
	}
	return documentExchange{status: response.StatusCode, body: responseBody}
}

func decodeIndexMetadata(body []byte) ([]IndexDetails, bool) {
	var entries map[string]json.RawMessage
	if json.Unmarshal(body, &entries) != nil || entries == nil {
		return nil, false
	}
	indices := make([]IndexDetails, 0, len(entries))
	for index, metadata := range entries {
		var fields map[string]json.RawMessage
		if !validIndexName(index) || json.Unmarshal(metadata, &fields) != nil || fields == nil || fields["settings"] == nil || fields["mappings"] == nil || fields["aliases"] == nil {
			return nil, false
		}
		indices = append(indices, IndexDetails{Index: index, Metadata: metadata})
	}
	slices.SortFunc(indices, func(left, right IndexDetails) int { return strings.Compare(left.Index, right.Index) })
	return indices, true
}

func isAcknowledged(body []byte) bool {
	var response struct {
		Acknowledged *bool `json:"acknowledged"`
	}
	return json.Unmarshal(body, &response) == nil && response.Acknowledged != nil && *response.Acknowledged
}

func indexErrorType(body []byte) string {
	var response struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &response) != nil {
		return ""
	}
	return response.Error.Type
}

func indexStatusFailureKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

func indexManagementMutationFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[IndexResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: operation, Message: message}
	return sdkgo.NewMutationBranch(branch, IndexResult{}, &failure, sdkgo.Receipt{})
}

func indexManagementMutationRetry(operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[IndexResult] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: operation, Message: message}
	return sdkgo.NewMutationRetry[IndexResult](failure, retryAfter)
}

func indexManagementQueryFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: operation, Message: message}
	var value T
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}

func indexManagementQueryRetry[T any](operation string, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "elasticsearch", Operation: operation, Message: message}
	return sdkgo.NewQueryRetry[T](failure, retryAfter)
}
