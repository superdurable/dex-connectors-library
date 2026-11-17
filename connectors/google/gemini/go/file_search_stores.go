// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

var fileSearchStoreNamePattern = regexp.MustCompile(`^fileSearchStores/[a-z0-9-]{1,40}$`)

// FileSearchStore contains the stable identity and selected metadata of a Gemini File Search store.
type FileSearchStore struct {
	// Name is the provider resource name, beginning with fileSearchStores/.
	Name string `json:"name"`
	// DisplayName is the human-readable name chosen at creation.
	DisplayName string `json:"displayName,omitempty"`
	// EmbeddingModel is the model used to index documents, when specified.
	EmbeddingModel string `json:"embeddingModel,omitempty"`
	// CreateTime is the provider's RFC 3339 creation timestamp.
	CreateTime string `json:"createTime,omitempty"`
}

// ListFileSearchStoresInput requests one bounded page of stores.
type ListFileSearchStoresInput struct {
	// PageSize is the desired number of stores; zero uses the provider default.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is the cursor returned by a previous page; blank starts at the beginning.
	PageToken string `json:"pageToken,omitempty"`
}

// FileSearchStoresPage contains one provider page and its next cursor.
type FileSearchStoresPage struct {
	// FileSearchStores contains the stores on this page.
	FileSearchStores []FileSearchStore `json:"fileSearchStores"`
	// NextPageToken is blank on the last page.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// GetFileSearchStoreInput identifies one store by its provider resource name.
type GetFileSearchStoreInput struct {
	// Name is a name such as fileSearchStores/my-store-abc123.
	Name string `json:"name"`
}

// CreateFileSearchStoreInput describes the store to create.
type CreateFileSearchStoreInput struct {
	// DisplayName is a nonempty human-readable name of at most 512 characters.
	DisplayName string `json:"displayName"`
	// EmbeddingModel is optional and has the form models/{model-id}.
	EmbeddingModel string `json:"embeddingModel,omitempty"`
}

// DeleteFileSearchStoreInput identifies a store and whether to delete its documents.
type DeleteFileSearchStoreInput struct {
	// Name is the provider resource name of the store.
	Name string `json:"name"`
	// Force also deletes the store's documents when true.
	Force bool `json:"force,omitempty"`
}

// DeleteFileSearchStoreOutput confirms which store was deleted.
type DeleteFileSearchStoreOutput struct {
	// Name is the requested store name.
	Name string `json:"name"`
}

// ListFileSearchStoresOperation reads a page of stores.
type ListFileSearchStoresOperation struct{ client *Client }

// Definition returns the generated listing contract.
func (ListFileSearchStoresOperation) Definition() sdkgo.QueryDefinition {
	return ListFileSearchStoresDefinition
}

// ListFileSearchStores binds this Query to the client.
func (client *Client) ListFileSearchStores() ListFileSearchStoresOperation {
	return ListFileSearchStoresOperation{client: client}
}

// Invoke lists stores with safe retry classification.
func (operation ListFileSearchStoresOperation) Invoke(call sdkgo.Call, input ListFileSearchStoresInput) sdkgo.QueryAttempt[FileSearchStoresPage] {
	if input.PageSize < 0 || input.PageSize > 20 || !validStorePageToken(input.PageToken) {
		return storeQueryBranch(ListFileSearchStoresBranchDefect, FileSearchStoresPage{}, sdkgo.FailureValidation, "file search store page input is invalid")
	}
	query := url.Values{}
	if input.PageSize > 0 {
		query.Set("pageSize", strconv.Itoa(input.PageSize))
	}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	path := "/fileSearchStores"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	response, failure := operation.client.doStoreRequest(call, http.MethodGet, path, nil)
	if failure != nil {
		return storeQueryFailure[FileSearchStoresPage](*failure, ListFileSearchStoresBranchDefect, ListFileSearchStoresBranchInvalidResponse)
	}
	if response.StatusCode != http.StatusOK {
		return storeQueryStatus[FileSearchStoresPage](response, ListFileSearchStoresBranchProviderRejected, ListFileSearchStoresBranchProviderRejected)
	}
	var page FileSearchStoresPage
	if err := json.Unmarshal(response.Body, &page); err != nil || !validStorePage(page) {
		return storeQueryBranch(ListFileSearchStoresBranchInvalidResponse, FileSearchStoresPage{}, sdkgo.FailureProtocol, "Gemini returned invalid file search stores JSON")
	}
	return sdkgo.NewQueryBranch(ListFileSearchStoresBranchListed, page, nil, sdkgo.Receipt{})
}

// GetFileSearchStoreOperation reads one store.
type GetFileSearchStoreOperation struct{ client *Client }

// Definition returns the generated get contract.
func (GetFileSearchStoreOperation) Definition() sdkgo.QueryDefinition {
	return GetFileSearchStoreDefinition
}

// GetFileSearchStore binds this Query to the client.
func (client *Client) GetFileSearchStore() GetFileSearchStoreOperation {
	return GetFileSearchStoreOperation{client: client}
}

// Invoke reads the named store.
func (operation GetFileSearchStoreOperation) Invoke(call sdkgo.Call, input GetFileSearchStoreInput) sdkgo.QueryAttempt[FileSearchStore] {
	if !fileSearchStoreNamePattern.MatchString(input.Name) {
		return storeQueryBranch(GetFileSearchStoreBranchDefect, FileSearchStore{}, sdkgo.FailureValidation, "file search store name is invalid")
	}
	response, failure := operation.client.doStoreRequest(call, http.MethodGet, "/"+input.Name, nil)
	if failure != nil {
		return storeQueryFailure[FileSearchStore](*failure, GetFileSearchStoreBranchDefect, GetFileSearchStoreBranchInvalidResponse)
	}
	if response.StatusCode == http.StatusNotFound {
		return storeQueryBranch(GetFileSearchStoreBranchNotFound, FileSearchStore{}, sdkgo.FailureNotFound, "file search store was not found")
	}
	if response.StatusCode != http.StatusOK {
		return storeQueryStatus[FileSearchStore](response, GetFileSearchStoreBranchProviderRejected, GetFileSearchStoreBranchProviderRejected)
	}
	var store FileSearchStore
	if err := json.Unmarshal(response.Body, &store); err != nil || store.Name != input.Name {
		return storeQueryBranch(GetFileSearchStoreBranchInvalidResponse, FileSearchStore{}, sdkgo.FailureProtocol, "Gemini returned invalid file search store JSON")
	}
	return sdkgo.NewQueryBranch(GetFileSearchStoreBranchFound, store, nil, sdkgo.Receipt{})
}

// CreateFileSearchStoreOperation creates a store with bounded repeat attempts.
type CreateFileSearchStoreOperation struct{ client *Client }

// Definition returns the generated create contract.
func (CreateFileSearchStoreOperation) Definition() sdkgo.MutationDefinition {
	return CreateFileSearchStoreDefinition
}

// IdempotencyKey identifies the logical create across retry attempts.
func (CreateFileSearchStoreOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateFileSearchStoreInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// CreateFileSearchStore binds this Mutation to the client.
func (client *Client) CreateFileSearchStore() CreateFileSearchStoreOperation {
	return CreateFileSearchStoreOperation{client: client}
}

// Invoke creates a store once per fenced dispatch and reads back after a possibly accepted request.
func (operation CreateFileSearchStoreOperation) Invoke(call sdkgo.Call, input CreateFileSearchStoreInput) sdkgo.MutationAttempt[FileSearchStore] {
	if len(input.DisplayName) == 0 || len(input.DisplayName) > 512 || strings.ContainsAny(input.DisplayName, "\r\n") ||
		(input.EmbeddingModel != "" && !strings.HasPrefix(input.EmbeddingModel, "models/")) {
		return storeMutationBranch(CreateFileSearchStoreBranchDefect, FileSearchStore{}, sdkgo.FailureValidation, "file search store display name or embedding model is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateFileSearchStoreDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return storeMutationBranch(CreateFileSearchStoreBranchDefect, FileSearchStore{}, sdkgo.FailureLocalDefect, "file search store checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return storeMutationBranch(CreateFileSearchStoreBranchDefect, FileSearchStore{}, sdkgo.FailureLocalDefect, "file search store checkpoint is invalid")
	}
	if decision.Kind != singledispatch.SendAllowed {
		store, found, failure := operation.client.findStoreByDisplayName(call, input)
		if found {
			return sdkgo.NewMutationBranch(CreateFileSearchStoreBranchCreated, store, nil, sdkgo.Receipt{CallID: call.ID, Provider: providerName, ProviderObjectID: store.Name, ObservedAt: time.Now().UTC()})
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			message := "Gemini create may have succeeded but no matching store was found after the send cap"
			if failure != nil {
				message = "Gemini create may have succeeded but store read-back failed after the send cap"
			}
			return storeMutationBranch(CreateFileSearchStoreBranchUncertain, FileSearchStore{}, sdkgo.FailureAvailability, message)
		}
	}
	body, _ := json.Marshal(input)
	var response storeHTTPResponse
	var failure *sdkgo.Failure
	if err := fence.Dispatch(func(dispatchContext context.Context) error {
		response, failure = operation.client.doStoreRequestWithContext(dispatchContext, call, http.MethodPost, "/fileSearchStores", body)
		return nil
	}); err != nil {
		return sdkgo.NewMutationRetry[FileSearchStore](storeFailure(sdkgo.FailureAvailability, "Gemini create could not be dispatched safely"), 0)
	}
	if failure != nil {
		if failure.Kind == sdkgo.FailureAuthentication {
			return storeMutationBranch(CreateFileSearchStoreBranchDefect, FileSearchStore{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[FileSearchStore](*failure, 0)
	}
	if response.StatusCode == http.StatusTooManyRequests {
		_ = fence.RecordCanResend()
		return sdkgo.NewMutationRetry[FileSearchStore](storeFailure(sdkgo.FailureRateLimit, "Gemini rate limited the create"), 0)
	}
	if response.StatusCode == http.StatusServiceUnavailable {
		return sdkgo.NewMutationRetry[FileSearchStore](storeFailure(sdkgo.FailureAvailability, "Gemini create outcome is unknown"), 0)
	}
	if response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[FileSearchStore](storeFailure(sdkgo.FailureAvailability, "Gemini create outcome is unknown"), 0)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return storeMutationBranch(CreateFileSearchStoreBranchProviderRejected, FileSearchStore{}, sdkgo.FailureProviderRejection, "Gemini rejected the create with HTTP "+strconv.Itoa(response.StatusCode))
	}
	var store FileSearchStore
	if err := json.Unmarshal(response.Body, &store); err != nil || !fileSearchStoreNamePattern.MatchString(store.Name) {
		return sdkgo.NewMutationRetry[FileSearchStore](storeFailure(sdkgo.FailureProtocol, "Gemini accepted the create but returned invalid store JSON"), 0)
	}
	_ = fence.RecordProviderID(store.Name)
	return sdkgo.NewMutationBranch(CreateFileSearchStoreBranchCreated, store, nil, sdkgo.Receipt{CallID: call.ID, Provider: providerName, ProviderObjectID: store.Name, ObservedAt: time.Now().UTC()})
}

// DeleteFileSearchStoreOperation removes a named store.
type DeleteFileSearchStoreOperation struct{ client *Client }

// Definition returns the generated delete contract.
func (DeleteFileSearchStoreOperation) Definition() sdkgo.MutationDefinition {
	return DeleteFileSearchStoreDefinition
}

// IdempotencyKey identifies the logical deletion across retry attempts.
func (DeleteFileSearchStoreOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteFileSearchStoreInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// DeleteFileSearchStore binds this Mutation to the client.
func (client *Client) DeleteFileSearchStore() DeleteFileSearchStoreOperation {
	return DeleteFileSearchStoreOperation{client: client}
}

// Invoke deletes the named store; a missing store is already deleted.
func (operation DeleteFileSearchStoreOperation) Invoke(call sdkgo.Call, input DeleteFileSearchStoreInput) sdkgo.MutationAttempt[DeleteFileSearchStoreOutput] {
	if !fileSearchStoreNamePattern.MatchString(input.Name) {
		return storeMutationBranch(DeleteFileSearchStoreBranchDefect, DeleteFileSearchStoreOutput{}, sdkgo.FailureValidation, "file search store name is invalid")
	}
	path := "/" + input.Name
	if input.Force {
		path += "?force=true"
	}
	response, failure := operation.client.doStoreRequest(call, http.MethodDelete, path, nil)
	if failure != nil {
		if failure.Kind == sdkgo.FailureAuthentication {
			return storeMutationBranch(DeleteFileSearchStoreBranchDefect, DeleteFileSearchStoreOutput{}, failure.Kind, failure.Message)
		}
		return sdkgo.NewMutationRetry[DeleteFileSearchStoreOutput](*failure, 0)
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return sdkgo.NewMutationRetry[DeleteFileSearchStoreOutput](storeFailure(sdkgo.FailureAvailability, "Gemini delete is temporarily unavailable"), 0)
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return storeMutationBranch(DeleteFileSearchStoreBranchProviderRejected, DeleteFileSearchStoreOutput{}, sdkgo.FailureProviderRejection, "Gemini rejected the delete with HTTP "+strconv.Itoa(response.StatusCode))
	}
	return sdkgo.NewMutationBranch(DeleteFileSearchStoreBranchDeleted, DeleteFileSearchStoreOutput{Name: input.Name}, nil, sdkgo.Receipt{CallID: call.ID, Provider: providerName, ProviderObjectID: input.Name, ObservedAt: time.Now().UTC()})
}

type storeHTTPResponse struct {
	StatusCode int
	Body       []byte
}

func (client *Client) doStoreRequest(call sdkgo.Call, method, path string, body []byte) (storeHTTPResponse, *sdkgo.Failure) {
	return client.doStoreRequestWithContext(call.Context, call, method, path, body)
}

func (client *Client) doStoreRequestWithContext(requestContext context.Context, call sdkgo.Call, method, path string, body []byte) (storeHTTPResponse, *sdkgo.Failure) {
	credential, err := client.credentials.Resolve(call)
	if err != nil || credential.Validate() != nil || !isValidAPIKey(credential.APIKey.Reveal()) {
		return storeHTTPResponse{}, failurePointer(sdkgo.FailureAuthentication, "Gemini credentials are unavailable")
	}
	request, err := http.NewRequestWithContext(requestContext, method, client.endpoint+path, bytes.NewReader(body))
	if err != nil {
		return storeHTTPResponse{}, failurePointer(sdkgo.FailureLocalDefect, "Gemini store request could not be built")
	}
	request.Header.Set("x-goog-api-key", credential.APIKey.Reveal())
	request.Header.Set("Accept", jsonMIMEType)
	if body != nil {
		request.Header.Set("Content-Type", jsonMIMEType)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return storeHTTPResponse{}, failurePointer(sdkgo.FailureTransport, "Gemini store request failed before a response was received")
	}
	defer response.Body.Close()
	limit := client.maxResponseBytes
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		limit = maxErrorResponseBytes
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return storeHTTPResponse{}, failurePointer(sdkgo.FailureTransport, "Gemini store response was interrupted")
	}
	if int64(len(content)) > limit && response.StatusCode >= 200 && response.StatusCode < 300 {
		return storeHTTPResponse{}, failurePointer(sdkgo.FailureResponseTooLarge, "Gemini store response exceeded the configured limit")
	}
	return storeHTTPResponse{StatusCode: response.StatusCode, Body: content}, nil
}

func (client *Client) findStoreByDisplayName(call sdkgo.Call, input CreateFileSearchStoreInput) (FileSearchStore, bool, *sdkgo.Failure) {
	pageToken := ""
	for pageNumber := 0; pageNumber < 100; pageNumber++ {
		query := url.Values{"pageSize": {"20"}}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		response, failure := client.doStoreRequest(call, http.MethodGet, "/fileSearchStores?"+query.Encode(), nil)
		if failure != nil {
			return FileSearchStore{}, false, failure
		}
		if response.StatusCode != http.StatusOK {
			return FileSearchStore{}, false, failurePointer(sdkgo.FailureAvailability, "Gemini store read-back was rejected")
		}
		var page FileSearchStoresPage
		if err := json.Unmarshal(response.Body, &page); err != nil || !validStorePage(page) {
			return FileSearchStore{}, false, failurePointer(sdkgo.FailureProtocol, "Gemini store read-back was invalid")
		}
		for _, store := range page.FileSearchStores {
			createdAt, err := time.Parse(time.RFC3339Nano, store.CreateTime)
			if err == nil && !createdAt.Before(call.Context.FlowStartedAt()) && store.DisplayName == input.DisplayName && store.EmbeddingModel == input.EmbeddingModel {
				return store, true, nil
			}
		}
		if page.NextPageToken == "" {
			return FileSearchStore{}, false, nil
		}
		pageToken = page.NextPageToken
	}
	return FileSearchStore{}, false, failurePointer(sdkgo.FailureAvailability, "Gemini store read-back exceeded 100 pages")
}

func validStorePageToken(token string) bool {
	return len(token) <= 4096 && !strings.ContainsAny(token, "\r\n")
}
func validStorePage(page FileSearchStoresPage) bool {
	if !validStorePageToken(page.NextPageToken) {
		return false
	}
	for _, store := range page.FileSearchStores {
		if !fileSearchStoreNamePattern.MatchString(store.Name) {
			return false
		}
	}
	return true
}

func storeQueryBranch[T any](branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	failure := storeFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, value, &failure, sdkgo.Receipt{})
}
func storeMutationBranch[T any](branch sdkgo.BranchID, value T, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	failure := storeFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, value, &failure, sdkgo.Receipt{})
}
func storeQueryFailure[T any](failure sdkgo.Failure, defectBranch, invalidBranch sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	if failure.Kind == sdkgo.FailureAuthentication || failure.Kind == sdkgo.FailureLocalDefect {
		return sdkgo.NewQueryBranch(defectBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	if failure.Kind == sdkgo.FailureResponseTooLarge {
		return sdkgo.NewQueryBranch(invalidBranch, *new(T), &failure, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryRetry[T](failure, 0)
}
func storeQueryStatus[T any](response storeHTTPResponse, rejectedBranch, _ sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return sdkgo.NewQueryRetry[T](storeFailure(sdkgo.FailureAvailability, "Gemini store request is temporarily unavailable"), 0)
	}
	return storeQueryBranch(rejectedBranch, *new(T), sdkgo.FailureProviderRejection, "Gemini rejected the store request with HTTP "+strconv.Itoa(response.StatusCode))
}
func storeFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: "fileSearchStores", Message: message}
}
