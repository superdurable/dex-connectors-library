// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azureblob

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// ListBlobsInput selects one container page, optional prefix, and continuation marker.
type ListBlobsInput struct {
	// Container is the lowercase Azure Storage container name.
	Container string `json:"container"`
	// Prefix limits names to the given path prefix; blank includes all names.
	Prefix string `json:"prefix,omitempty"`
	// Marker resumes from a prior BlobsPage; blank starts at the beginning.
	Marker string `json:"marker,omitempty"`
}

// BlobsPage contains bounded blob names and an optional continuation marker.
type BlobsPage struct {
	// Names are the names on this page.
	Names []string `json:"names"`
	// NextMarker is blank when the list is complete.
	NextMarker string `json:"nextMarker"`
}

// ListBlobsOperation reads a page of blob names.
type ListBlobsOperation struct{ client *Client }

// ListBlobs binds the Query to this client.
func (client *Client) ListBlobs() ListBlobsOperation { return ListBlobsOperation{client} }

// Definition returns the manifest-generated Query contract.
func (ListBlobsOperation) Definition() sdkgo.QueryDefinition { return ListBlobsDefinition }

// Invoke reads one XML listing page and classifies Azure outcomes.
func (operation ListBlobsOperation) Invoke(call sdkgo.Call, input ListBlobsInput) sdkgo.QueryAttempt[BlobsPage] {
	if !validContainer(input.Container) || len(input.Prefix) > 1024 || strings.ContainsAny(input.Prefix, "\x00\r\n") || len(input.Marker) > 4096 || strings.ContainsAny(input.Marker, "\x00\r\n") {
		return listBlobsBranch(ListBlobsBranchDefect, sdkgo.FailureValidation, "container, prefix, or marker is invalid")
	}
	query := url.Values{"restype": {"container"}, "comp": {"list"}, "maxresults": {strconv.FormatInt(operation.client.listPageSize, 10)}}
	if input.Prefix != "" {
		query.Set("prefix", input.Prefix)
	}
	if input.Marker != "" {
		query.Set("marker", input.Marker)
	}
	response := operation.client.requestStorage(call, http.MethodGet, "/"+input.Container+"?"+query.Encode(), nil, "", nil)
	if errors.Is(response.err, errCredential) {
		return listBlobsBranch(ListBlobsBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable")
	}
	if shouldRetryStorage(response) {
		return sdkgo.NewQueryRetry[BlobsPage](storageFailure("listBlobs", storageRetryKind(response), "Azure Blob list is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return sdkgo.NewQueryBranch(ListBlobsBranchMissing, BlobsPage{Names: []string{}}, nil, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		return listBlobsBranch(ListBlobsBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil {
		return listBlobsBranch(ListBlobsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Azure Blob list exceeded the configured limit")
	}
	var envelope struct {
		XMLName xml.Name `xml:"EnumerationResults"`
		Blobs   struct {
			Blob []struct {
				Name string `xml:"Name"`
			} `xml:"Blob"`
		} `xml:"Blobs"`
		NextMarker string `xml:"NextMarker"`
	}
	if xml.Unmarshal(response.body, &envelope) != nil || envelope.XMLName.Local != "EnumerationResults" || len(envelope.Blobs.Blob) > int(operation.client.listPageSize) || len(envelope.NextMarker) > 4096 {
		return listBlobsBranch(ListBlobsBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned invalid listing XML")
	}
	page := BlobsPage{Names: make([]string, 0, len(envelope.Blobs.Blob)), NextMarker: envelope.NextMarker}
	for _, item := range envelope.Blobs.Blob {
		if !isValidBlobName(item.Name) {
			return listBlobsBranch(ListBlobsBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned an invalid name")
		}
		page.Names = append(page.Names, item.Name)
	}
	return sdkgo.NewQueryBranch(ListBlobsBranchListed, page, nil, sdkgo.Receipt{})
}

// PutBlobInput replaces one block blob with bounded base64-decoded content.
type PutBlobInput struct {
	// Container is the lowercase Azure Storage container name.
	Container string `json:"container"`
	// BlobName is the path inside the container.
	BlobName string `json:"blobName"`
	// DataBase64 is the complete blob body encoded with standard base64.
	DataBase64 string `json:"dataBase64"`
	// ContentType is the non-secret HTTP media type, such as text/plain.
	ContentType string `json:"contentType"`
}

// BlobWriteResult contains the ETag Azure assigned to the stored blob.
type BlobWriteResult struct {
	// ETag identifies the stored version for conditional follow-up calls.
	ETag string `json:"etag"`
}

// PutBlobOperation creates or replaces one block blob.
type PutBlobOperation struct{ client *Client }

// PutBlob binds the Mutation to this client.
func (client *Client) PutBlob() PutBlobOperation { return PutBlobOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (PutBlobOperation) Definition() sdkgo.MutationDefinition { return PutBlobDefinition }

// IdempotencyKey identifies an absolute replacement of one blob name.
func (PutBlobOperation) IdempotencyKey(callID sdkgo.CallID, _ PutBlobInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke writes one complete block blob and returns its ETag.
func (operation PutBlobOperation) Invoke(call sdkgo.Call, input PutBlobInput) sdkgo.MutationAttempt[BlobWriteResult] {
	if !validContainer(input.Container) || !isValidBlobName(input.BlobName) || input.DataBase64 == "" ||
		!isSafeResponseHeader(input.ContentType) || !strings.Contains(input.ContentType, "/") {
		return putBlobBranch(PutBlobBranchDefect, sdkgo.FailureValidation, "blob write input is invalid")
	}
	body, err := base64.StdEncoding.Strict().DecodeString(input.DataBase64)
	if err != nil || int64(len(body)) > operation.client.maxResponseBytes {
		return putBlobBranch(PutBlobBranchDefect, sdkgo.FailureValidation, "blob body is invalid or too large")
	}
	headers := http.Header{"x-ms-blob-type": {"BlockBlob"}}
	response := operation.client.requestStorage(call, http.MethodPut, blobPath(input.Container, input.BlobName), body, input.ContentType, headers)
	if errors.Is(response.err, errCredential) {
		return putBlobBranch(PutBlobBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable")
	}
	if shouldRetryStorage(response) {
		return sdkgo.NewMutationRetry[BlobWriteResult](storageFailure("putBlob", storageRetryKind(response), "Azure Blob write is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusCreated {
		return putBlobBranch(PutBlobBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
	}
	if response.err != nil || response.etag == "" || !isSafeResponseHeader(response.etag) {
		return putBlobBranch(PutBlobBranchInvalidResponse, sdkgo.FailureProtocol, "Azure Blob returned invalid write metadata")
	}
	return sdkgo.NewMutationBranch(PutBlobBranchStored, BlobWriteResult{ETag: response.etag}, nil, sdkgo.Receipt{})
}

// DeleteBlobInput identifies one blob to remove.
type DeleteBlobInput struct {
	// Container is the lowercase Azure Storage container name.
	Container string `json:"container"`
	// BlobName is the path inside the container.
	BlobName string `json:"blobName"`
}

// BlobDeleteResult reports whether Azure accepted a deletion request.
type BlobDeleteResult struct {
	// Deleted is true when Azure accepted deletion or the blob was already absent.
	Deleted bool `json:"deleted"`
}

// DeleteBlobOperation removes one blob and its snapshots.
type DeleteBlobOperation struct{ client *Client }

// DeleteBlob binds the Mutation to this client.
func (client *Client) DeleteBlob() DeleteBlobOperation { return DeleteBlobOperation{client} }

// Definition returns the manifest-generated Mutation contract.
func (DeleteBlobOperation) Definition() sdkgo.MutationDefinition { return DeleteBlobDefinition }

// IdempotencyKey identifies a repeat-safe deletion by blob name.
func (DeleteBlobOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteBlobInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes one blob and treats an absent blob as already deleted.
func (operation DeleteBlobOperation) Invoke(call sdkgo.Call, input DeleteBlobInput) sdkgo.MutationAttempt[BlobDeleteResult] {
	if !validContainer(input.Container) || !isValidBlobName(input.BlobName) {
		return deleteBlobBranch(DeleteBlobBranchDefect, sdkgo.FailureValidation, "blob identity is invalid")
	}
	response := operation.client.requestStorage(call, http.MethodDelete, blobPath(input.Container, input.BlobName), nil, "", http.Header{"x-ms-delete-snapshots": {"include"}})
	if errors.Is(response.err, errCredential) {
		return deleteBlobBranch(DeleteBlobBranchDefect, sdkgo.FailureAuthentication, "Azure Blob account key is unavailable")
	}
	if shouldRetryStorage(response) {
		return sdkgo.NewMutationRetry[BlobDeleteResult](storageFailure("deleteBlob", storageRetryKind(response), "Azure Blob delete is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusAccepted || response.status == http.StatusNotFound {
		return sdkgo.NewMutationBranch(DeleteBlobBranchDeleted, BlobDeleteResult{Deleted: true}, nil, sdkgo.Receipt{})
	}
	return deleteBlobBranch(DeleteBlobBranchProviderRejected, sdkgo.FailureProviderRejection, "Azure Blob rejected HTTP "+strconv.Itoa(response.status))
}

func validContainer(container string) bool {
	return containerPattern.MatchString(container) && !strings.Contains(container, "--")
}

func shouldRetryStorage(response providerResponse) bool {
	return errors.Is(response.err, errRequest) || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500
}

func storageRetryKind(response providerResponse) sdkgo.FailureKind {
	if response.status == http.StatusTooManyRequests {
		return sdkgo.FailureRateLimit
	}
	if response.status >= 500 {
		return sdkgo.FailureAvailability
	}
	return sdkgo.FailureTransport
}

func storageFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "microsoft-azure-blob", Operation: operation, Message: message}
}

func listBlobsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[BlobsPage] {
	failure := storageFailure("listBlobs", kind, message)
	return sdkgo.NewQueryBranch(branch, BlobsPage{Names: []string{}}, &failure, sdkgo.Receipt{})
}

func putBlobBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[BlobWriteResult] {
	failure := storageFailure("putBlob", kind, message)
	return sdkgo.NewMutationBranch(branch, BlobWriteResult{}, &failure, sdkgo.Receipt{})
}

func deleteBlobBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[BlobDeleteResult] {
	failure := storageFailure("deleteBlob", kind, message)
	return sdkgo.NewMutationBranch(branch, BlobDeleteResult{}, &failure, sdkgo.Receipt{})
}
