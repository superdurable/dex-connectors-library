// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drive_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// accountHierarchyPage is the research fixture's live match: the archived twin and the
// trashed draft are decoys that the exact, non-trashed, folder-scoped query must exclude.
const accountHierarchyPage = `{"files":[{"id":"file_ah","name":"Account Hierarchy","mimeType":"application/vnd.google-apps.spreadsheet",` +
	`"parents":["fld_sales"],"modifiedTime":"2026-01-20T09:00:00.000Z","webViewLink":"https://docs.google.com/spreadsheets/d/file_ah/edit"}]}`

func TestSearchFilesSendsAnExactEscapedNonTrashedQuery(t *testing.T) {
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, accountHierarchyPage)
	})
	client := newDriveClient(t, fake.URL)

	result, err := sdkgo.RunQuery(newDexContext("search-exact"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{
		Name: `Account Hierarchy`, MimeType: "application/vnd.google-apps.spreadsheet", ParentFolderID: "fld_sales",
	})
	require.NoError(t, err)
	require.Equal(t, drive.SearchFilesBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	require.Equal(t, []drive.FileSummary{{
		ID: "file_ah", Name: "Account Hierarchy", MimeType: "application/vnd.google-apps.spreadsheet", Parents: []string{"fld_sales"},
		ModifiedTime: time.Date(2026, time.January, 20, 9, 0, 0, 0, time.UTC), WebViewLink: "https://docs.google.com/spreadsheets/d/file_ah/edit",
	}}, result.Value.Files)
	require.Equal(t, "google-drive", result.Receipt.Provider)
	require.Equal(t, "google-request", result.Receipt.ProviderRequestID)

	requests := fake.recorded()
	require.Len(t, requests, 1)
	request := requests[0]
	require.Equal(t, "/drive/v3/files", request.path)
	require.Equal(t, "Bearer "+driveTestToken, request.header.Get("Authorization"))
	require.Equal(t, []string{"name = 'Account Hierarchy' and mimeType = 'application/vnd.google-apps.spreadsheet' and 'fld_sales' in parents and trashed = false"}, request.query["q"])
	require.Equal(t, []string{"25"}, request.query["pageSize"])
	require.Equal(t, []string{"modifiedTime desc,name"}, request.query["orderBy"])
	require.Equal(t, []string{"allDrives"}, request.query["corpora"])
	require.Equal(t, []string{"true"}, request.query["supportsAllDrives"])
	require.Equal(t, []string{"true"}, request.query["includeItemsFromAllDrives"])
	require.Empty(t, request.query["pageToken"])
}

func TestSearchFilesEscapesQuotesAndBackslashesAndUsesDrivePrefixMatching(t *testing.T) {
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, accountHierarchyPage)
	})
	client := newDriveClient(t, fake.URL)

	_, err := sdkgo.RunQuery(newDexContext("search-contains"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{
		Name: `Valentine's \ Plan' or name contains '`, NameMatch: drive.NameMatchContains, PageSize: 7, PageToken: "next-token",
	})
	require.NoError(t, err)
	request := fake.recorded()[0]
	require.Equal(t, []string{`name contains 'Valentine\'s \\ Plan\' or name contains \'' and trashed = false`}, request.query["q"])
	require.Equal(t, []string{"7"}, request.query["pageSize"])
	require.Equal(t, []string{"next-token"}, request.query["pageToken"])
}

func TestSearchFilesSelectsNotFoundOnlyWhenNoPagesRemain(t *testing.T) {
	body := `{"files":[]}`
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, body)
	})
	client := newDriveClient(t, fake.URL)

	missing, err := sdkgo.RunQuery(newDexContext("search-missing"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{Name: "Acount Hierarchy"})
	require.NoError(t, err)
	require.Equal(t, drive.SearchFilesBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	require.Empty(t, missing.Value.Files)

	body = `{"files":[],"nextPageToken":"more","incompleteSearch":true}`
	morePages, err := sdkgo.RunQuery(newDexContext("search-more-pages"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{Name: "Account Hierarchy"})
	require.NoError(t, err)
	require.Equal(t, drive.SearchFilesBranchFound, morePages.Branch)
	require.Equal(t, "more", morePages.Value.NextPageToken)
	require.True(t, morePages.Value.IsIncompleteSearch)
}

func TestSearchFilesRejectsInvalidInputWithoutProviderRequest(t *testing.T) {
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, accountHierarchyPage)
	})
	client := newDriveClient(t, fake.URL)
	for name, input := range map[string]drive.SearchFilesInput{
		"whitespace name":         {Name: "   "},
		"unknown name match":      {Name: "Plan", NameMatch: "fuzzy"},
		"name match without name": {NameMatch: drive.NameMatchContains},
		"uppercase MIME type":     {MimeType: "Text/CSV"},
		"MIME type parameters":    {MimeType: "text/plain; charset=utf-8"},
		"MIME type without slash": {MimeType: "spreadsheet"},
		"parent folder injection": {ParentFolderID: "x' in parents or 'y"},
		"page size above limit":   {PageSize: 101},
		"negative page size":      {PageSize: -1},
		"page token with spaces":  {PageToken: "a b"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newDexContext("search-invalid-"+name), client.SearchFiles(), driveConnection, input)
			require.NoError(t, err)
			require.Equal(t, drive.SearchFilesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, fake.recorded())
}

func TestSearchFilesRetriesRateLimitsAndOutages(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		retryAfter string
		wantKind   sdkgo.FailureKind
		wantDelay  time.Duration
	}{
		{name: "429 with Retry-After", status: http.StatusTooManyRequests, retryAfter: "7", wantKind: sdkgo.FailureRateLimit, wantDelay: 7 * time.Second},
		{name: "403 user rate limit", status: http.StatusForbidden, body: `{"error":{"errors":[{"reason":"userRateLimitExceeded"}],"code":403}}`, wantKind: sdkgo.FailureRateLimit},
		{name: "503 outage", status: http.StatusServiceUnavailable, body: `{}`, wantKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			client := newDriveClient(t, fake.URL)
			_, err := sdkgo.RunQuery(newDexContext("search-retry"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{Name: "Plan"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.wantKind, retry.Failure.Kind)
			var delayedRetry *dex.RetryAfterError
			if test.wantDelay == 0 {
				require.False(t, errors.As(err, &delayedRetry))
				return
			}
			require.ErrorAs(t, err, &delayedRetry)
			require.Equal(t, test.wantDelay, delayedRetry.After)
		})
	}
}

func TestSearchFilesClassifiesRejectedAndInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		config     drive.Config
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{name: "unknown parent folder", status: http.StatusNotFound, body: `{"error":{"errors":[{"reason":"notFound","message":"File not found: SENTINEL"}]}}`, wantBranch: drive.SearchFilesBranchProviderRejected, wantKind: sdkgo.FailureNotFound},
		{name: "invalid page token", status: http.StatusBadRequest, body: `{"error":{"errors":[{"reason":"invalid"}]}}`, wantBranch: drive.SearchFilesBranchProviderRejected, wantKind: sdkgo.FailureProviderRejection},
		{name: "insufficient permission", status: http.StatusForbidden, body: `{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`, wantBranch: drive.SearchFilesBranchProviderRejected, wantKind: sdkgo.FailureAuthorization},
		{name: "malformed JSON", status: http.StatusOK, body: `{"files":`, wantBranch: drive.SearchFilesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "file without ID", status: http.StatusOK, body: `{"files":[{"name":"Plan","mimeType":"text/plain"}]}`, wantBranch: drive.SearchFilesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "more files than the page size", status: http.StatusOK, body: `{"files":[{"id":"a","name":"A","mimeType":"text/plain"},{"id":"b","name":"B","mimeType":"text/plain"}]}`, config: drive.Config{SearchPageSize: 1}, wantBranch: drive.SearchFilesBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "oversized response", status: http.StatusOK, body: accountHierarchyPage, config: drive.Config{MaxResponseBytes: 16}, wantBranch: drive.SearchFilesBranchInvalidResponse, wantKind: sdkgo.FailureResponseTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newDriveClient(t, fake.URL, test.config)
			result, err := sdkgo.RunQuery(newDexContext("search-rejected"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{Name: "Plan", ParentFolderID: "folder"})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.Len(t, fake.recorded(), 1)
		})
	}
}

func TestSearchFilesRefreshesOnceAfterUnauthorizedAndNeverLoops(t *testing.T) {
	for _, test := range []struct {
		name          string
		isReplacement bool
		wantBranch    sdkgo.BranchID
	}{
		{name: "replacement accepted", isReplacement: true, wantBranch: drive.SearchFilesBranchFound},
		{name: "replacement rejected", wantBranch: drive.SearchFilesBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeDrive(t, func(response http.ResponseWriter, request *http.Request, _ []byte) {
				if test.isReplacement && request.Header.Get("Authorization") == "Bearer replacement-token" {
					writeJSON(t, response, http.StatusOK, accountHierarchyPage)
					return
				}
				writeJSON(t, response, http.StatusUnauthorized, `{}`)
			})
			provider := &rejectionRefreshingCredentialProvider{}
			client, err := drive.New(drive.Config{Endpoint: fake.URL}, provider)
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newDexContext("search-refresh"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{Name: "Plan"})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Len(t, fake.recorded(), 2)
			require.Equal(t, 1, provider.forcedRefreshes)
		})
	}
}

func TestSearchFilesWithoutCredentialsUsesDefect(t *testing.T) {
	fake := newFakeDrive(t, func(response http.ResponseWriter, _ *http.Request, _ []byte) {
		writeJSON(t, response, http.StatusOK, accountHierarchyPage)
	})
	client, err := drive.New(drive.Config{Endpoint: fake.URL}, sdkgo.StaticCredentialProvider[drive.Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newDexContext("search-no-credentials"), client.SearchFiles(), driveConnection, drive.SearchFilesInput{Name: "Plan"})
	require.NoError(t, err)
	require.Equal(t, drive.SearchFilesBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, fake.recorded())
}

type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (drive.Credentials, error) {
	return drive.Credentials{AccessToken: sdkgo.NewSecretString("rejected-token")}, nil
}

// ResolveWithRefresh returns the stored credential unchanged: its expiry has not passed.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[drive.Credentials],
) (drive.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[drive.Credentials],
) (drive.Credentials, error) {
	provider.forcedRefreshes++
	return drive.Credentials{AccessToken: sdkgo.NewSecretString("replacement-token")}, nil
}

func TestNewRejectsUnsafeEndpointsAndOutOfRangeLimits(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[drive.Credentials]{}
	for name, config := range map[string]drive.Config{
		"plain HTTP remote endpoint": {Endpoint: "http://drive.example.com"},
		"endpoint with query":        {Endpoint: "https://www.googleapis.com/?key=secret"},
		"upload above multipart":     {MaxUploadBytes: 5<<20 + 1},
		"page size above limit":      {SearchPageSize: 101},
		"negative text limit":        {MaxTextBytes: -1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := drive.New(config, credentials)
			require.Error(t, err)
			require.False(t, strings.Contains(err.Error(), "secret"))
		})
	}
	_, err := drive.New(drive.Config{}, nil)
	require.Error(t, err)
	client, err := drive.New(drive.Config{}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}
