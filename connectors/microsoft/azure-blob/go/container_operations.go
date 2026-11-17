// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azureblob

import (
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListContainersInput selects a page of storage account containers.
type ListContainersInput struct {
	// Prefix filters container names; blank includes all containers.
	Prefix string `json:"prefix,omitempty"`
	// Marker resumes from a prior ContainersPage; blank starts at the beginning.
	Marker string `json:"marker,omitempty"`
}

// ContainersPage contains bounded container names and a continuation marker.
type ContainersPage struct {
	// Names are the container names on this page.
	Names []string `json:"names"`
	// NextMarker is blank when the list is complete.
	NextMarker string `json:"nextMarker"`
}

// ListContainersOperation reads one page of container names.
type ListContainersOperation struct{ client *Client }

// ListContainers binds the Query to this client.
func (client *Client) ListContainers() ListContainersOperation {
	return ListContainersOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListContainersOperation) Definition() sdkgo.QueryDefinition { return ListContainersDefinition }

// Invoke reads one account-level XML listing page.
func (operation ListContainersOperation) Invoke(call sdkgo.Call, input ListContainersInput) sdkgo.QueryAttempt[ContainersPage] {
	if len(input.Prefix) > 63 || strings.ContainsAny(input.Prefix, "\x00\r\n") || len(input.Marker) > 4096 || strings.ContainsAny(input.Marker, "\x00\r\n") {
		return listContainersBranch(ListContainersBranchDefect, sdkgo.FailureValidation, "container prefix or marker is invalid")
	}
	query := url.Values{"comp": {"list"}, "maxresults": {strconv.FormatInt(operation.client.listPageSize, 10)}}
	if input.Prefix != "" {
		query.Set("prefix", input.Prefix)
	}
	if input.Marker != "" {
		query.Set("marker", input.Marker)
	}
	response := operation.client.requestStorage(call, http.MethodGet, "/?"+query.Encode(), nil, "", nil)
	if errors.Is(response.err, errCredential) {
		return listContainersBranch(ListContainersBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable")
	}
	if shouldRetryStorage(response) {
		return sdkgo.NewQueryRetry[ContainersPage](storageFailure("listContainers", storageRetryKind(response), "Azure container list is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listContainersBranch(ListContainersBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return listContainersBranch(ListContainersBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Azure container list exceeded the configured limit")
	}
	var envelope struct {
		XMLName    xml.Name `xml:"EnumerationResults"`
		Containers struct {
			Container []struct {
				Name string `xml:"Name"`
			} `xml:"Container"`
		} `xml:"Containers"`
		NextMarker string `xml:"NextMarker"`
	}
	if xml.Unmarshal(response.body, &envelope) != nil || envelope.XMLName.Local != "EnumerationResults" || len(envelope.Containers.Container) > int(operation.client.listPageSize) || len(envelope.NextMarker) > 4096 {
		return listContainersBranch(ListContainersBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned invalid container XML")
	}
	page := ContainersPage{Names: make([]string, 0, len(envelope.Containers.Container)), NextMarker: envelope.NextMarker}
	for _, item := range envelope.Containers.Container {
		if !validContainer(item.Name) {
			return listContainersBranch(ListContainersBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned an invalid container name")
		}
		page.Names = append(page.Names, item.Name)
	}
	return sdkgo.NewQueryBranch(ListContainersBranchListed, page, nil, sdkgo.Receipt{})
}

// GetContainerInput identifies one storage account container.
type GetContainerInput struct {
	// Container is the lowercase Azure Storage container name.
	Container string `json:"container"`
}

// ContainerProperties contains version and modification metadata.
type ContainerProperties struct {
	// ETag identifies this version of the container properties.
	ETag string `json:"etag"`
	// LastModified is Azure's RFC 1123 timestamp.
	LastModified string `json:"lastModified"`
}

// GetContainerOperation reads one container's properties.
type GetContainerOperation struct{ client *Client }

// GetContainer binds the Query to this client.
func (client *Client) GetContainer() GetContainerOperation { return GetContainerOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetContainerOperation) Definition() sdkgo.QueryDefinition { return GetContainerDefinition }

// Invoke performs a signed HEAD request and validates Azure metadata.
func (operation GetContainerOperation) Invoke(call sdkgo.Call, input GetContainerInput) sdkgo.QueryAttempt[ContainerProperties] {
	if !validContainer(input.Container) {
		return getContainerBranch(GetContainerBranchDefect, sdkgo.FailureValidation, "container name is invalid")
	}
	response := operation.client.requestStorage(call, http.MethodHead, "/"+input.Container+"?restype=container", nil, "", nil)
	if errors.Is(response.err, errCredential) {
		return getContainerBranch(GetContainerBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable")
	}
	if shouldRetryStorage(response) {
		return sdkgo.NewQueryRetry[ContainerProperties](storageFailure("getContainer", storageRetryKind(response), "Azure container lookup is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetContainerBranchMissing, ContainerProperties{}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return getContainerBranch(GetContainerBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil || response.etag == "" || !isSafeResponseHeader(response.etag) || !isSafeResponseHeader(response.lastModified) {
		return getContainerBranch(GetContainerBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned invalid container metadata")
	}
	if _, err := time.Parse(http.TimeFormat, response.lastModified); err != nil {
		return getContainerBranch(GetContainerBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned invalid modification time")
	}
	return sdkgo.NewQueryBranch(GetContainerBranchFound, ContainerProperties{ETag: response.etag, LastModified: response.lastModified}, nil, sdkgo.Receipt{})
}

// CreateContainerInput names one container to create.
type CreateContainerInput struct {
	// Container is the new lowercase Azure Storage container name.
	Container string `json:"container"`
}

// ContainerWriteResult identifies the created container.
type ContainerWriteResult struct {
	// Container is the storage account container name.
	Container string `json:"container"`
}

// CreateContainerOperation creates one uniquely named container.
type CreateContainerOperation struct{ client *Client }

// CreateContainer binds the Mutation to this client.
func (client *Client) CreateContainer() CreateContainerOperation {
	return CreateContainerOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreateContainerOperation) Definition() sdkgo.MutationDefinition {
	return CreateContainerDefinition
}

// IdempotencyKey identifies a named create that cannot create duplicate containers.
func (CreateContainerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateContainerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one container, distinguishing an existing name from success.
func (operation CreateContainerOperation) Invoke(call sdkgo.Call, input CreateContainerInput) sdkgo.MutationAttempt[ContainerWriteResult] {
	if !validContainer(input.Container) {
		return createContainerBranch(CreateContainerBranchDefect, sdkgo.FailureValidation, "container name is invalid")
	}
	response := operation.client.requestStorage(call, http.MethodPut, "/"+input.Container+"?restype=container", nil, "", nil)
	if errors.Is(response.err, errCredential) {
		return createContainerBranch(CreateContainerBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable")
	}
	if shouldRetryStorage(response) {
		return sdkgo.NewMutationRetry[ContainerWriteResult](storageFailure("createContainer", storageRetryKind(response), "Azure container create is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusConflict {
		return createContainerBranch(CreateContainerBranchAlreadyExists, sdkgo.FailureConflict, "Azure Blob container already exists")
	}
	if response.status != http.StatusCreated {
		return createContainerBranch(CreateContainerBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return createContainerBranch(CreateContainerBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob container response is unreadable")
	}
	return sdkgo.NewMutationBranch(CreateContainerBranchCreated, ContainerWriteResult{Container: input.Container}, nil, sdkgo.Receipt{})
}

// DeleteContainerInput identifies a container and all of its blobs for deletion.
type DeleteContainerInput struct {
	// Container is the lowercase Azure Storage container name.
	Container string `json:"container"`
}

// ContainerDeleteResult reports acceptance of container deletion.
type ContainerDeleteResult struct {
	// Deleted is true when Azure accepted deletion or the container was absent.
	Deleted bool `json:"deleted"`
}

// DeleteContainerOperation removes one container and all blobs inside it.
type DeleteContainerOperation struct{ client *Client }

// DeleteContainer binds the Mutation to this client.
func (client *Client) DeleteContainer() DeleteContainerOperation {
	return DeleteContainerOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (DeleteContainerOperation) Definition() sdkgo.MutationDefinition {
	return DeleteContainerDefinition
}

// IdempotencyKey identifies a repeat-safe deletion by container name.
func (DeleteContainerOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteContainerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one container and treats a missing container as already deleted.
func (operation DeleteContainerOperation) Invoke(call sdkgo.Call, input DeleteContainerInput) sdkgo.MutationAttempt[ContainerDeleteResult] {
	if !validContainer(input.Container) {
		return deleteContainerBranch(DeleteContainerBranchDefect, sdkgo.FailureValidation, "container name is invalid")
	}
	response := operation.client.requestStorage(call, http.MethodDelete, "/"+input.Container+"?restype=container", nil, "", nil)
	if errors.Is(response.err, errCredential) {
		return deleteContainerBranch(DeleteContainerBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable")
	}
	if shouldRetryStorage(response) {
		return sdkgo.NewMutationRetry[ContainerDeleteResult](storageFailure("deleteContainer", storageRetryKind(response), "Azure container delete is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusAccepted || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteContainerBranchDeleted, ContainerDeleteResult{Deleted: true}, nil, sdkgo.Receipt{})
	}
	return deleteContainerBranch(DeleteContainerBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
}

func listContainersBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ContainersPage] {
	failure := storageFailure("listContainers", kind, message)
	return sdkgo.NewQueryBranch(branch, ContainersPage{Names: []string{}}, &failure, sdkgo.Receipt{})
}

func getContainerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ContainerProperties] {
	failure := storageFailure("getContainer", kind, message)
	return sdkgo.NewQueryBranch(branch, ContainerProperties{}, &failure, sdkgo.Receipt{})
}

func createContainerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContainerWriteResult] {
	failure := storageFailure("createContainer", kind, message)
	return sdkgo.NewMutationBranch(branch, ContainerWriteResult{}, &failure, sdkgo.Receipt{})
}

func deleteContainerBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ContainerDeleteResult] {
	failure := storageFailure("deleteContainer", kind, message)
	return sdkgo.NewMutationBranch(branch, ContainerDeleteResult{}, &failure, sdkgo.Receipt{})
}
