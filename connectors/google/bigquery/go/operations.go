// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bigquery

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

// ListDatasetsInput selects one bounded page of datasets in a Google Cloud project.
type ListDatasetsInput struct {
	// ProjectID is the Google Cloud project ID shown in BigQuery Studio.
	ProjectID string `json:"projectId"`
	// MaxResults limits this page to 1 through 100 datasets; zero uses 100.
	MaxResults int `json:"maxResults,omitempty"`
	// PageToken is returned by the previous page; blank selects the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// Dataset identifies a BigQuery dataset without exposing its full metadata.
type Dataset struct {
	// ID is the dataset ID within ProjectID.
	ID string `json:"id"`
	// ProjectID is the project containing this dataset.
	ProjectID string `json:"projectId"`
	// FriendlyName is an optional display name.
	FriendlyName string `json:"friendlyName"`
}

// DatasetsPage holds one page of dataset identities and its next cursor.
type DatasetsPage struct {
	// Datasets are visible to the authorized Google identity.
	Datasets []Dataset `json:"datasets"`
	// NextPageToken is blank on the final page.
	NextPageToken string `json:"nextPageToken"`
}

// ListDatasetsOperation reads accessible datasets in one project.
type ListDatasetsOperation struct{ client *Client }

// ListDatasets binds the dataset Query to this client.
func (client *Client) ListDatasets() ListDatasetsOperation { return ListDatasetsOperation{client} }

// Definition returns the manifest-generated dataset listing contract.
func (ListDatasetsOperation) Definition() sdkgo.QueryDefinition { return ListDatasetsDefinition }

// Invoke requests a bounded page and projects dataset identities.
func (operation ListDatasetsOperation) Invoke(call sdkgo.Call, input ListDatasetsInput) sdkgo.QueryAttempt[DatasetsPage] {
	if !validProjectID(input.ProjectID) || input.MaxResults < 0 || input.MaxResults > 100 || !validPageToken(input.PageToken) {
		return datasetBranch(ListDatasetsBranchDefect, sdkgo.FailureValidation, "project ID, page size, or page token is invalid")
	}
	if input.MaxResults == 0 {
		input.MaxResults = 100
	}
	query := url.Values{"maxResults": {strconv.Itoa(input.MaxResults)}}
	if input.PageToken != "" {
		query.Set("pageToken", input.PageToken)
	}
	path := "/bigquery/v2/projects/" + url.PathEscape(input.ProjectID) + "/datasets?" + query.Encode()
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return datasetBranch(ListDatasetsBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return datasetRetry(sdkgo.FailureTransport, "BigQuery request failed", 0)
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return datasetBranch(ListDatasetsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "BigQuery response exceeded the configured limit")
	}
	if response.err != nil {
		return datasetRetry(sdkgo.FailureTransport, "BigQuery response was interrupted", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return datasetRetry(sdkgo.FailureRateLimit, "BigQuery rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return datasetRetry(sdkgo.FailureAvailability, "BigQuery is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return datasetBranch(ListDatasetsBranchProviderRejected, sdkgo.FailureProviderRejection, "BigQuery rejected HTTP "+strconv.Itoa(response.status))
	}
	page, err := decodeDatasetsPage(response.body, input)
	if err != nil {
		return datasetBranch(ListDatasetsBranchInvalidResponse, sdkgo.FailureProtocol, "BigQuery returned invalid dataset JSON")
	}
	return sdkgo.NewQueryBranch(ListDatasetsBranchListed, page, nil, sdkgo.Receipt{})
}

var projectIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:._~-]{0,255}$`)
var datasetIDPattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func validProjectID(projectID string) bool { return projectIDPattern.MatchString(projectID) }
func validPageToken(pageToken string) bool {
	return len(pageToken) <= 4096 && !strings.ContainsAny(pageToken, "\r\n")
}

func decodeDatasetsPage(body []byte, input ListDatasetsInput) (DatasetsPage, error) {
	var payload struct {
		Datasets []struct {
			DatasetReference struct {
				DatasetID string `json:"datasetId"`
				ProjectID string `json:"projectId"`
			} `json:"datasetReference"`
			FriendlyName string `json:"friendlyName"`
		} `json:"datasets"`
		NextPageToken string `json:"nextPageToken"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return DatasetsPage{}, err
	}
	if len(payload.Datasets) > input.MaxResults || !validPageToken(payload.NextPageToken) {
		return DatasetsPage{}, fmt.Errorf("BigQuery dataset page is invalid")
	}
	page := DatasetsPage{Datasets: make([]Dataset, 0, len(payload.Datasets)), NextPageToken: payload.NextPageToken}
	for _, item := range payload.Datasets {
		if len(item.DatasetReference.DatasetID) > 1024 || !datasetIDPattern.MatchString(item.DatasetReference.DatasetID) || item.DatasetReference.ProjectID != input.ProjectID {
			return DatasetsPage{}, fmt.Errorf("BigQuery dataset reference is invalid")
		}
		page.Datasets = append(page.Datasets, Dataset{ID: item.DatasetReference.DatasetID, ProjectID: item.DatasetReference.ProjectID, FriendlyName: item.FriendlyName})
	}
	return page, nil
}

func datasetBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[DatasetsPage] {
	failure := datasetFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, DatasetsPage{}, &failure, sdkgo.Receipt{})
}

func datasetRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[DatasetsPage] {
	return sdkgo.NewQueryRetry[DatasetsPage](datasetFailure(kind, message), delay)
}

func datasetFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "google-bigquery", Operation: "listDatasets", Message: message}
}
