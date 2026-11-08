// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package firestore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListDocumentsInput selects one bounded collection page in the default Firestore database.
type ListDocumentsInput struct {
	// ProjectID is the Google Cloud project ID shown in Firestore Databases.
	ProjectID string `json:"projectId"`
	// CollectionID is the top-level collection ID shown in Firestore Documents.
	CollectionID string `json:"collectionId"`
	// PageSize limits this page to 1 through 100 documents; zero uses 100.
	PageSize int `json:"pageSize,omitempty"`
	// PageToken is returned by the previous page; blank selects the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// Document identifies a Firestore document by its full resource name.
type Document struct {
	// Name has the form projects/{project-id}/databases/(default)/documents/{collection-id}/{document-id}.
	Name string `json:"name"`
}

// DocumentsPage holds one page of document names and its next cursor.
type DocumentsPage struct {
	// Documents are visible to the authorized Google identity.
	Documents []Document `json:"documents"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListDocumentsOperation reads accessible documents in one project.
type ListDocumentsOperation struct{ client *Client }

// ListDocuments binds the document Query to this client.
func (client *Client) ListDocuments() ListDocumentsOperation { return ListDocumentsOperation{client} }

// Definition returns the manifest-generated document listing contract.
func (ListDocumentsOperation) Definition() sdkgo.QueryDefinition { return ListDocumentsDefinition }

// Invoke requests a bounded page and projects document names.
func (operation ListDocumentsOperation) Invoke(call sdkgo.Call, input ListDocumentsInput) sdkgo.QueryAttempt[DocumentsPage] {
	if !validProjectID(input.ProjectID) || !validCollectionID(input.CollectionID) || input.PageSize < 0 || input.PageSize > 100 || !validPageToken(input.PageToken) {
		return documentBranch(ListDocumentsBranchDefect, sdkgo.FailureValidation, "project ID, collection ID, page size, or page token is invalid")
	}
	if input.PageSize == 0 {
		input.PageSize = 100
	}
	query := url.Values{"pageSize": {strconv.Itoa(input.PageSize)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	path := "/v1/projects/" + url.PathEscape(input.ProjectID) + "/databases/(default)/documents/" + url.PathEscape(input.CollectionID) + "?" + query.Encode()
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return documentBranch(ListDocumentsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return documentRetry(sdkgo.FailureTransport, "Firestore request failed", 0)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return documentBranch(ListDocumentsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Firestore response exceeded the configured limit")
	}
	if response.err != nil {
		return documentRetry(sdkgo.FailureTransport, "Firestore response was interrupted", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return documentRetry(sdkgo.FailureRateLimit, "Firestore rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return documentRetry(sdkgo.FailureAvailability, "Firestore is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return documentBranch(ListDocumentsBranchProviderRejected, sdkgo.FailureProviderRejection, "Firestore rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeDocumentsPage(response.body, input)
	if err != nil {
		return documentBranch(ListDocumentsBranchInvalidResponse, sdkgo.FailureProtocol, "Firestore returned invalid document JSON")
	}
	return sdkgo.NewQueryBranch(ListDocumentsBranchListed, page, nil, sdkgo.Receipt{})
}

var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}$`)

func validProjectID(projectID string) bool { return projectIDPattern.MatchString(projectID) }

var collectionIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.~-]{0,255}$`)

func validCollectionID(collectionID string) bool {
	return collectionIDPattern.MatchString(collectionID)
}
func validPageToken(pageToken string) bool {
	return len(pageToken) <= 4096 && !strings.ContainsAny(pageToken, "\r\n")
}

func decodeDocumentsPage(body []byte, input ListDocumentsInput) (DocumentsPage, error) {
	var payload struct {
		Documents []struct {
			Name string `json:"name"`
		} `json:"documents"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return DocumentsPage{}, err
	}
	if len(payload.Documents) > input.PageSize || !validPageToken(payload.NextPageToken) {
		return DocumentsPage{}, fmt.Errorf("Firestore document page is invalid")
	}
	page := DocumentsPage{Documents: make([]Document, 0, len(payload.Documents)), NextPageToken: payload.NextPageToken}
	prefix := "projects/" + input.ProjectID + "/databases/(default)/documents/" + input.CollectionID + "/"
	for _, item := range payload.Documents {
		documentID := strings.TrimPrefix(item.Name, prefix)
		if documentID == item.Name || documentID == "" || len(documentID) > 255 || strings.ContainsAny(documentID, "/\r\n") {
			return DocumentsPage{}, fmt.Errorf("Firestore document name is invalid")
		}
		page.Documents = append(page.Documents, Document{Name: item.Name})
	}
	return page, nil
}

func documentBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DocumentsPage] {
	failure := documentFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, DocumentsPage{}, &failure, sdkgo.Receipt{})
}

func documentRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[DocumentsPage] {
	return sdkgo.NewQueryRetry[DocumentsPage](documentFailure(kind, message), delay)
}

func documentFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "google-firestore", Operation: "listDocuments", Message: message}
}
