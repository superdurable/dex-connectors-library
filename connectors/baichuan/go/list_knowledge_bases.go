// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package baichuan

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListKnowledgeBasesInput requests all knowledge bases visible to the connection's API key.
type ListKnowledgeBasesInput struct{}

// KnowledgeBase is the list metadata for one Baichuan knowledge base.
type KnowledgeBase struct {
	// ID is the stable provider ID used by knowledge-base APIs.
	ID string `json:"id"`
	// Name is the provider's knowledge-base name.
	Name string `json:"name"`
	// Description is the provider's knowledge-base description.
	Description string `json:"description"`
	// Status is the provider's processing state such as init, parsing, online, or fail.
	Status string `json:"status"`
	// FileIDs lists the provider file IDs attached to the knowledge base.
	FileIDs []string `json:"fileIds"`
	// CreatedAt is the provider creation time in Unix seconds.
	CreatedAt int64 `json:"createdAt"`
	// UpdatedAt is the provider update time in Unix seconds.
	UpdatedAt int64 `json:"updatedAt"`
}

// ListKnowledgeBasesOutput contains all knowledge bases returned by Baichuan's unpaginated list API.
type ListKnowledgeBasesOutput struct {
	// KnowledgeBases is empty when the provider returned no knowledge bases.
	KnowledgeBases []KnowledgeBase `json:"knowledgeBases"`
}

// ListKnowledgeBasesOperation reads the knowledge-base list.
type ListKnowledgeBasesOperation struct{ client *Client }

// Definition returns the generated knowledge-base list contract.
func (ListKnowledgeBasesOperation) Definition() sdkgo.QueryDefinition {
	return ListKnowledgeBasesDefinition
}

// Invoke lists knowledge bases and classifies provider and transport outcomes.
func (operation ListKnowledgeBasesOperation) Invoke(call sdkgo.Call, _ ListKnowledgeBasesInput) sdkgo.QueryAttempt[ListKnowledgeBasesOutput] {
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !isValidAPIKey(credentials.APIKey.Reveal()) {
		return listKnowledgeBasesBranch(ListKnowledgeBasesBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/kbs", nil)
	if err != nil {
		return listKnowledgeBasesBranch(ListKnowledgeBasesBranchDefect, sdkgo.FailureValidation, "knowledge-base request is invalid")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return listKnowledgeBasesRetry(sdkgo.FailureAvailability, "Baichuan request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return listKnowledgeBasesRetry(sdkgo.FailureRateLimit, "Baichuan rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= http.StatusInternalServerError {
		return listKnowledgeBasesRetry(sdkgo.FailureAvailability, "Baichuan is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		kind := sdkgo.FailureProviderRejection
		if response.StatusCode == http.StatusUnauthorized {
			kind = sdkgo.FailureAuthentication
		}
		if response.StatusCode == http.StatusForbidden {
			kind = sdkgo.FailureAuthorization
		}
		return listKnowledgeBasesBranch(ListKnowledgeBasesBranchProviderRejected, kind, "Baichuan rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return listKnowledgeBasesBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Baichuan response exceeded the configured limit")
	}
	if err != nil {
		return listKnowledgeBasesRetry(sdkgo.FailureAvailability, "Baichuan response was interrupted", 0)
	}
	output, err := decodeListKnowledgeBasesResponse(body)
	if err != nil {
		return listKnowledgeBasesBranch(ListKnowledgeBasesBranchInvalidResponse, sdkgo.FailureProtocol, "Baichuan returned an invalid knowledge-base list")
	}
	return sdkgo.NewQueryBranch(ListKnowledgeBasesBranchListed, output, nil, sdkgo.Receipt{})
}

func decodeListKnowledgeBasesResponse(body []byte) (ListKnowledgeBasesOutput, error) {
	var wire struct {
		Object string `json:"object"`
		Data   []struct {
			ID          string   `json:"id"`
			Object      string   `json:"object"`
			Name        string   `json:"name"`
			Description string   `json:"description"`
			Status      string   `json:"status"`
			FileIDs     []string `json:"file_ids"`
			CreatedAt   int64    `json:"created_at"`
			UpdatedAt   int64    `json:"updated_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Object != "list" || wire.Data == nil {
		return ListKnowledgeBasesOutput{}, errors.New("invalid Baichuan list envelope")
	}
	if len(wire.Data) > 10000 {
		return ListKnowledgeBasesOutput{}, errors.New("Baichuan list exceeds limit")
	}
	output := ListKnowledgeBasesOutput{KnowledgeBases: make([]KnowledgeBase, 0, len(wire.Data))}
	seen := make(map[string]struct{}, len(wire.Data))
	for _, item := range wire.Data {
		if item.ID == "" || len(item.ID) > 256 || item.Object != "knowledge-base" || len(item.Name) > 256 || len(item.Description) > 4096 || item.CreatedAt < 0 || item.UpdatedAt < 0 || len(item.FileIDs) > 10000 {
			return ListKnowledgeBasesOutput{}, errors.New("invalid Baichuan knowledge base")
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return ListKnowledgeBasesOutput{}, errors.New("duplicate Baichuan knowledge base")
		}
		seen[item.ID] = struct{}{}
		for _, fileID := range item.FileIDs {
			if fileID == "" || len(fileID) > 256 {
				return ListKnowledgeBasesOutput{}, errors.New("invalid Baichuan file ID")
			}
		}
		output.KnowledgeBases = append(output.KnowledgeBases, KnowledgeBase{ID: item.ID, Name: item.Name, Description: item.Description, Status: item.Status, FileIDs: item.FileIDs, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt})
	}
	return output, nil
}
func listKnowledgeBasesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ListKnowledgeBasesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "baichuan", Operation: "listKnowledgeBases", Message: message}
	return sdkgo.NewQueryBranch(branch, ListKnowledgeBasesOutput{KnowledgeBases: []KnowledgeBase{}}, &failure, sdkgo.Receipt{})
}
func listKnowledgeBasesRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[ListKnowledgeBasesOutput] {
	failure := sdkgo.Failure{Kind: kind, Provider: "baichuan", Operation: "listKnowledgeBases", Message: message}
	return sdkgo.NewQueryRetry[ListKnowledgeBasesOutput](failure, delay)
}
