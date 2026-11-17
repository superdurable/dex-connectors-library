// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinecone

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// FetchVectorsInput selects IDs from one index namespace.
type FetchVectorsInput struct {
	// Namespace selects the index namespace; blank uses Pinecone's default.
	Namespace string `json:"namespace,omitempty"`
	// IDs contains one or more vector IDs.
	IDs []string `json:"ids"`
}

// FetchedVector contains an ID and dense values, excluding stored metadata.
type FetchedVector struct {
	// ID is the stable vector ID.
	ID string `json:"id"`
	// Values contains the dense coordinates.
	Values []float32 `json:"values"`
}

// FetchedVectors is the bounded set returned by Pinecone.
type FetchedVectors struct {
	// Namespace is the requested index namespace.
	Namespace string `json:"namespace"`
	// Vectors contains matching IDs, sorted by ID.
	Vectors []FetchedVector `json:"vectors"`
}

// ListVectorIDsInput selects a page in a serverless index namespace.
type ListVectorIDsInput struct {
	// Namespace selects the index namespace; blank uses Pinecone's default.
	Namespace string `json:"namespace,omitempty"`
	// Prefix restricts IDs to this prefix; blank includes all IDs.
	Prefix string `json:"prefix,omitempty"`
	// Limit is a positive page size; zero uses Pinecone's default of 100.
	Limit int `json:"limit,omitempty"`
	// PaginationToken continues a previous page; blank starts at the first page.
	PaginationToken string `json:"paginationToken,omitempty"`
}

// VectorIDPage is one sorted page of vector IDs.
type VectorIDPage struct {
	// IDs contains the returned vector IDs.
	IDs []string `json:"ids"`
	// NextToken is blank at the end of the listing.
	NextToken string `json:"nextToken"`
}

// DescribeIndexStatsInput requests statistics for the configured index.
type DescribeIndexStatsInput struct{}

// IndexStats summarizes index dimension and vector counts.
type IndexStats struct {
	// Dimension is the configured dense-vector dimension.
	Dimension int `json:"dimension"`
	// TotalVectorCount is the sum of vectors across namespaces.
	TotalVectorCount int `json:"totalVectorCount"`
}

// UpdateVectorInput sets dense values or selected metadata on one vector.
type UpdateVectorInput struct {
	// Namespace selects the index namespace; blank uses Pinecone's default.
	Namespace string `json:"namespace,omitempty"`
	// ID is the existing vector ID.
	ID string `json:"id"`
	// Values replace the vector coordinates when nonempty.
	Values []float32 `json:"values,omitempty"`
	// SetMetadata adds or replaces these string-valued metadata fields.
	SetMetadata map[string]string `json:"setMetadata,omitempty"`
}

// DeleteVectorsInput selects IDs to remove from one namespace.
type DeleteVectorsInput struct {
	// Namespace selects the index namespace; blank uses Pinecone's default.
	Namespace string `json:"namespace,omitempty"`
	// IDs contains one or more vector IDs.
	IDs []string `json:"ids"`
}

// ClearNamespaceInput names the namespace to empty.
type ClearNamespaceInput struct {
	// Namespace must be an explicit nonempty namespace.
	Namespace string `json:"namespace"`
}

// VectorAction records the namespace and IDs affected by a write.
type VectorAction struct {
	// Namespace is the target index namespace.
	Namespace string `json:"namespace"`
	// IDs contains targeted IDs; empty for a whole-namespace clear.
	IDs []string `json:"ids,omitempty"`
}

// FetchVectorsOperation reads selected vector IDs.
type FetchVectorsOperation struct{ client *Client }

// FetchVectors binds the Query to this client.
func (client *Client) FetchVectors() FetchVectorsOperation { return FetchVectorsOperation{client} }

// Definition returns the generated Query contract.
func (FetchVectorsOperation) Definition() sdkgo.QueryDefinition { return FetchVectorsDefinition }

// Invoke fetches selected vectors without returning their metadata.
func (operation FetchVectorsOperation) Invoke(call sdkgo.Call, input FetchVectorsInput) sdkgo.QueryAttempt[FetchedVectors] {
	if err := validateVectorIDs(input.IDs); err != nil {
		return vectorQueryFailure[FetchedVectors]("fetchVectors", FetchVectorsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	query := url.Values{"ids": input.IDs}
	if input.Namespace != "" {
		query.Set("namespace", input.Namespace)
	}
	response := operation.client.send(call, "fetchVectors", http.MethodGet, "/vectors/fetch", query, nil, false, http.StatusOK)
	if attempt, done := classifyVectorQuery[FetchedVectors](response); done {
		return attempt
	}
	var record struct {
		Vectors map[string]struct {
			ID     string    `json:"id"`
			Values []float32 `json:"values"`
		} `json:"vectors"`
	}
	if json.Unmarshal(response.body, &record) != nil || record.Vectors == nil {
		return vectorQueryFailure[FetchedVectors]("fetchVectors", FetchVectorsBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid vectors")
	}
	output := FetchedVectors{Namespace: input.Namespace, Vectors: make([]FetchedVector, 0, len(record.Vectors))}
	for id, vector := range record.Vectors {
		if id == "" || vector.ID != id || vector.Values == nil {
			return vectorQueryFailure[FetchedVectors]("fetchVectors", FetchVectorsBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid vector data")
		}
		output.Vectors = append(output.Vectors, FetchedVector{ID: id, Values: vector.Values})
	}
	sort.Slice(output.Vectors, func(i, j int) bool { return output.Vectors[i].ID < output.Vectors[j].ID })
	return sdkgo.NewQueryBranch(FetchVectorsBranchFetched, output, nil, sdkgo.Receipt{})
}

// ListVectorIDsOperation reads vector IDs from a serverless index.
type ListVectorIDsOperation struct{ client *Client }

// ListVectorIDs binds the Query to this client.
func (client *Client) ListVectorIDs() ListVectorIDsOperation { return ListVectorIDsOperation{client} }

// Definition returns the generated Query contract.
func (ListVectorIDsOperation) Definition() sdkgo.QueryDefinition { return ListVectorIDsDefinition }

// Invoke lists one page of IDs; Pinecone supports this endpoint only on serverless indexes.
func (operation ListVectorIDsOperation) Invoke(call sdkgo.Call, input ListVectorIDsInput) sdkgo.QueryAttempt[VectorIDPage] {
	if input.Limit < 0 || input.Limit > 1000 || len(input.PaginationToken) > 4096 || strings.Contains(input.Prefix, " ") {
		return vectorQueryFailure[VectorIDPage]("listVectorIDs", ListVectorIDsBranchDefect, sdkgo.FailureValidation, "vector ID list filters are invalid")
	}
	query := url.Values{}
	if input.Namespace != "" {
		query.Set("namespace", input.Namespace)
	}
	if input.Prefix != "" {
		query.Set("prefix", input.Prefix)
	}
	if input.Limit > 0 {
		query.Set("limit", fmt.Sprint(input.Limit))
	}
	if input.PaginationToken != "" {
		query.Set("paginationToken", input.PaginationToken)
	}
	response := operation.client.send(call, "listVectorIDs", http.MethodGet, "/vectors/list", query, nil, false, http.StatusOK)
	if attempt, done := classifyVectorQuery[VectorIDPage](response); done {
		return attempt
	}
	var record struct {
		Vectors []struct {
			ID string `json:"id"`
		} `json:"vectors"`
		Pagination struct {
			Next string `json:"next"`
		} `json:"pagination"`
	}
	if json.Unmarshal(response.body, &record) != nil || record.Vectors == nil {
		return vectorQueryFailure[VectorIDPage]("listVectorIDs", ListVectorIDsBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid vector IDs")
	}
	output := VectorIDPage{IDs: make([]string, 0, len(record.Vectors)), NextToken: record.Pagination.Next}
	for _, vector := range record.Vectors {
		if vector.ID == "" {
			return vectorQueryFailure[VectorIDPage]("listVectorIDs", ListVectorIDsBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned an empty vector ID")
		}
		output.IDs = append(output.IDs, vector.ID)
	}
	return sdkgo.NewQueryBranch(ListVectorIDsBranchListed, output, nil, sdkgo.Receipt{})
}

// DescribeIndexStatsOperation reads index counts.
type DescribeIndexStatsOperation struct{ client *Client }

// DescribeIndexStats binds the Query to this client.
func (client *Client) DescribeIndexStats() DescribeIndexStatsOperation {
	return DescribeIndexStatsOperation{client}
}

// Definition returns the generated Query contract.
func (DescribeIndexStatsOperation) Definition() sdkgo.QueryDefinition {
	return DescribeIndexStatsDefinition
}

// Invoke reads the index dimension and total vector count.
func (operation DescribeIndexStatsOperation) Invoke(call sdkgo.Call, _ DescribeIndexStatsInput) sdkgo.QueryAttempt[IndexStats] {
	response := operation.client.send(call, "describeIndexStats", http.MethodPost, "/describe_index_stats", nil, nil, false, http.StatusOK)
	if attempt, done := classifyVectorQuery[IndexStats](response); done {
		return attempt
	}
	if !hasResponseShape(response.body, "dimension") || !hasResponseShape(response.body, "totalVectorCount") {
		return vectorQueryFailure[IndexStats]("describeIndexStats", DescribeIndexStatsBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned incomplete index statistics")
	}
	var output IndexStats
	if json.Unmarshal(response.body, &output) != nil || output.Dimension < 0 || output.TotalVectorCount < 0 {
		return vectorQueryFailure[IndexStats]("describeIndexStats", DescribeIndexStatsBranchInvalidResponse, sdkgo.FailureProtocol, "provider returned invalid index statistics")
	}
	return sdkgo.NewQueryBranch(DescribeIndexStatsBranchDescribed, output, nil, sdkgo.Receipt{})
}

// UpdateVectorOperation changes one vector's values or selected metadata.
type UpdateVectorOperation struct{ client *Client }

// UpdateVector binds the Mutation to this client.
func (client *Client) UpdateVector() UpdateVectorOperation { return UpdateVectorOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateVectorOperation) Definition() sdkgo.MutationDefinition { return UpdateVectorDefinition }

// IdempotencyKey derives a stable Dex call key for the receipt.
func (UpdateVectorOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateVectorInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces values and sets named metadata fields.
func (operation UpdateVectorOperation) Invoke(call sdkgo.Call, input UpdateVectorInput) sdkgo.MutationAttempt[VectorAction] {
	if input.ID == "" || (len(input.Values) == 0 && len(input.SetMetadata) == 0) {
		return vectorMutationFailure("updateVector", UpdateVectorBranchDefect, sdkgo.FailureValidation, "vector ID and values or metadata are required")
	}
	body, err := json.Marshal(struct {
		ID          string            `json:"id"`
		Namespace   string            `json:"namespace,omitempty"`
		Values      []float32         `json:"values,omitempty"`
		SetMetadata map[string]string `json:"setMetadata,omitempty"`
	}{input.ID, input.Namespace, input.Values, input.SetMetadata})
	if err != nil {
		return vectorMutationFailure("updateVector", UpdateVectorBranchDefect, sdkgo.FailureValidation, "vector update is invalid")
	}
	return executeVectorMutation(operation.client, call, "updateVector", "/vectors/update", body, VectorAction{Namespace: input.Namespace, IDs: []string{input.ID}}, UpdateVectorBranchUpdated)
}

// DeleteVectorsOperation removes selected IDs from one namespace.
type DeleteVectorsOperation struct{ client *Client }

// DeleteVectors binds the Mutation to this client.
func (client *Client) DeleteVectors() DeleteVectorsOperation { return DeleteVectorsOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteVectorsOperation) Definition() sdkgo.MutationDefinition { return DeleteVectorsDefinition }

// IdempotencyKey derives a stable Dex call key for the receipt.
func (DeleteVectorsOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteVectorsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes selected IDs; repeating the same delete is harmless.
func (operation DeleteVectorsOperation) Invoke(call sdkgo.Call, input DeleteVectorsInput) sdkgo.MutationAttempt[VectorAction] {
	if err := validateVectorIDs(input.IDs); err != nil {
		return vectorMutationFailure("deleteVectors", DeleteVectorsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	body, err := json.Marshal(input)
	if err != nil {
		return vectorMutationFailure("deleteVectors", DeleteVectorsBranchDefect, sdkgo.FailureValidation, "vector IDs are invalid")
	}
	return executeVectorMutation(operation.client, call, "deleteVectors", "/vectors/delete", body, VectorAction{Namespace: input.Namespace, IDs: input.IDs}, DeleteVectorsBranchDeleted)
}

// ClearNamespaceOperation deletes all vectors from one explicit namespace.
type ClearNamespaceOperation struct{ client *Client }

// ClearNamespace binds the Mutation to this client.
func (client *Client) ClearNamespace() ClearNamespaceOperation {
	return ClearNamespaceOperation{client}
}

// Definition returns the generated Mutation contract.
func (ClearNamespaceOperation) Definition() sdkgo.MutationDefinition { return ClearNamespaceDefinition }

// IdempotencyKey derives a stable Dex call key for the receipt.
func (ClearNamespaceOperation) IdempotencyKey(callID sdkgo.CallID, _ ClearNamespaceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes all vectors in the named namespace.
func (operation ClearNamespaceOperation) Invoke(call sdkgo.Call, input ClearNamespaceInput) sdkgo.MutationAttempt[VectorAction] {
	if input.Namespace == "" {
		return vectorMutationFailure("clearNamespace", ClearNamespaceBranchDefect, sdkgo.FailureValidation, "an explicit namespace is required")
	}
	body, err := json.Marshal(struct {
		Namespace string `json:"namespace"`
		DeleteAll bool   `json:"deleteAll"`
	}{input.Namespace, true})
	if err != nil {
		return vectorMutationFailure("clearNamespace", ClearNamespaceBranchDefect, sdkgo.FailureValidation, "namespace is invalid")
	}
	return executeVectorMutation(operation.client, call, "clearNamespace", "/vectors/delete", body, VectorAction{Namespace: input.Namespace}, ClearNamespaceBranchCleared)
}

func validateVectorIDs(ids []string) error {
	if len(ids) == 0 || len(ids) > 1000 {
		return fmt.Errorf("vector IDs must contain 1 through 1000 items")
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("vector ID cannot be blank")
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("vector IDs must be distinct")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func vectorQueryFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryBranch(branch, *new(T), failure(operation, kind, message), sdkgo.Receipt{})
}

func classifyVectorQuery[T any](response providerResponse) (sdkgo.QueryAttempt[T], bool) {
	if response.retry {
		return sdkgo.NewQueryRetry[T](*response.failure, response.retryAfter), true
	}
	if response.failure != nil {
		return sdkgo.NewQueryBranch(response.branch, *new(T), response.failure, sdkgo.Receipt{}), true
	}
	return sdkgo.QueryAttempt[T]{}, false
}

func vectorMutationFailure(operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[VectorAction] {
	return sdkgo.NewMutationBranch(branch, VectorAction{}, failure(operation, kind, message), sdkgo.Receipt{})
}

func executeVectorMutation(client *Client, call sdkgo.Call, operation, path string, body []byte, output VectorAction, success sdkgo.BranchID) sdkgo.MutationAttempt[VectorAction] {
	response := client.send(call, operation, http.MethodPost, path, nil, body, false, http.StatusOK)
	if response.retry {
		return sdkgo.NewMutationRetry[VectorAction](*response.failure, response.retryAfter)
	}
	if response.failure != nil {
		return sdkgo.NewMutationBranch(response.branch, VectorAction{}, response.failure, sdkgo.Receipt{})
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(response.body, &object) != nil || object == nil {
		return vectorMutationFailure(operation, "invalidResponse", sdkgo.FailureProtocol, "provider returned invalid action response")
	}
	return sdkgo.NewMutationBranch(success, output, nil, sdkgo.Receipt{IdempotencyKey: call.IdempotencyKey})
}
