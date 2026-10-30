// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	defaultMergedPullRequestPageSize = 30
	defaultPullRequestFilePageSize   = 50
	defaultCommitPageSize            = 30
	// GitHub search serves only its first 1000 matches, so later pages fail before provider access.
	searchResultLimit       = 1000
	maximumPathBytes        = 4096
	maximumRefBytes         = 255
	maximumFilenameBytes    = 4096
	maximumFileStatusBytes  = 32
	maximumTitleBytes       = 1024
	maximumLabels           = 100
	maximumLabelBytes       = 256
	maximumAuthorNameBytes  = 256
	maximumAuthorLoginBytes = 100
)

var (
	repositoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	commitSHAPattern      = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	linkRelationPattern   = regexp.MustCompile(`<([^>]*)>\s*;\s*rel="([^"]*)"`)
)

// ListMergedPullRequestsInput selects one page of pull requests merged within an inclusive time window.
type ListMergedPullRequestsInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// MergedAfter is the inclusive window start. GitHub search receives it in UTC with second precision.
	MergedAfter time.Time `json:"mergedAfter"`
	// MergedBefore is the inclusive window end and must be after MergedAfter. GitHub search receives it in
	// UTC with second precision.
	MergedBefore time.Time `json:"mergedBefore"`
	// PageSize is 1 through 100. Zero selects 30.
	PageSize int `json:"pageSize,omitempty"`
	// Page is one-based. Zero selects 1.
	Page int `json:"page,omitempty"`
}

// MergedPullRequestPage is one bounded GitHub search page. NextPage is zero when no page follows.
type MergedPullRequestPage struct {
	// PullRequests holds this page's merged pull requests, newest created first.
	PullRequests []MergedPullRequest `json:"pullRequests"`
	// TotalCount is GitHub's count of every matching pull request. It can exceed the 1000 results that
	// GitHub search serves; narrow the window to read them all.
	TotalCount int `json:"totalCount"`
	// IncompleteResults reports that GitHub search timed out before it found every match.
	IncompleteResults bool `json:"incompleteResults"`
	// NextPage is the one-based page that follows, or zero when this is the last page.
	NextPage int `json:"nextPage"`
}

// MergedPullRequest is one merged pull request with bounded text.
type MergedPullRequest struct {
	// Number is the pull request number within its repository.
	Number int `json:"number"`
	// Title is the trimmed pull request title, bounded to 1024 bytes.
	Title string `json:"title"`
	// Body is the beginning of the pull request body, bounded by the maxPullRequestBodyCharacters
	// configuration field.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports that Body omits the end of the pull request body.
	BodyTruncated bool `json:"bodyTruncated"`
	// URL is the pull request's GitHub web URL, or empty when GitHub returns an unsafe URL.
	URL string `json:"url,omitempty"`
	// AuthorLogin is the author's GitHub login, or empty when GitHub omits the author.
	AuthorLogin string `json:"authorLogin,omitempty"`
	// MergedAt is the merge time in UTC.
	MergedAt time.Time `json:"mergedAt"`
	// Labels holds up to 100 non-empty label names, each bounded to 256 bytes.
	Labels []string `json:"labels,omitempty"`
}

// ListPullRequestFilesInput selects one page of files changed by one pull request.
type ListPullRequestFilesInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// Number is the positive pull request number.
	Number int `json:"number"`
	// PageSize is 1 through 100. Zero selects 50.
	PageSize int `json:"pageSize,omitempty"`
	// Page is one-based. Zero selects 1.
	Page int `json:"page,omitempty"`
}

// PullRequestFilePage is one bounded page of changed files. NextPage is zero when no page follows.
type PullRequestFilePage struct {
	// Files holds this page's changed files in GitHub's order.
	Files []PullRequestFile `json:"files"`
	// NextPage is the one-based page that follows, or zero when this is the last page.
	NextPage int `json:"nextPage"`
}

// PullRequestFile is one file changed by a pull request, with a bounded patch.
type PullRequestFile struct {
	// Filename is the file's repository path after the change.
	Filename string `json:"filename"`
	// PreviousFilename is the file's path before a rename, or empty.
	PreviousFilename string `json:"previousFilename,omitempty"`
	// Status is GitHub's change status, such as added, modified, removed, or renamed.
	Status string `json:"status"`
	// Additions is the number of added lines.
	Additions int `json:"additions"`
	// Deletions is the number of removed lines.
	Deletions int `json:"deletions"`
	// Changes is the number of changed lines.
	Changes int `json:"changes"`
	// Patch is the beginning of the file's unified diff, bounded by the maxPatchCharacters configuration
	// field. GitHub omits the patch for a binary or very large file, which leaves Patch empty.
	Patch string `json:"patch,omitempty"`
	// PatchTruncated reports that Patch omits the end of GitHub's patch.
	PatchTruncated bool `json:"patchTruncated"`
}

// ListCommitsInput selects one page of commits within a time window, optionally from one ref and path.
type ListCommitsInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// Since is the window start. GitHub receives it as since, in UTC with second precision.
	Since time.Time `json:"since"`
	// Until is the window end and must be after Since. GitHub receives it as until, in UTC with second
	// precision.
	Until time.Time `json:"until"`
	// Path limits commits to those that changed one file or directory path.
	Path string `json:"path,omitempty"`
	// Ref is a branch, tag, or commit SHA. Empty selects the repository default branch.
	Ref string `json:"ref,omitempty"`
	// PageSize is 1 through 100. Zero selects 30.
	PageSize int `json:"pageSize,omitempty"`
	// Page is one-based. Zero selects 1.
	Page int `json:"page,omitempty"`
}

// CommitPage is one bounded page of commits. NextPage is zero when no page follows.
type CommitPage struct {
	// Commits holds this page's commits in GitHub's order, newest first.
	Commits []CommitSummary `json:"commits"`
	// NextPage is the one-based page that follows, or zero when this is the last page.
	NextPage int `json:"nextPage"`
}

// CommitSummary is one commit with a bounded message. It never contains commit email addresses.
type CommitSummary struct {
	// SHA is the commit's lowercase hexadecimal SHA-1 or SHA-256 object name.
	SHA string `json:"sha"`
	// Message is the beginning of the commit message, bounded by the maxCommitMessageCharacters
	// configuration field.
	Message string `json:"message"`
	// MessageTruncated reports that Message omits the end of the commit message.
	MessageTruncated bool `json:"messageTruncated"`
	// AuthorLogin is the GitHub login linked to the commit author, or empty when GitHub links none.
	AuthorLogin string `json:"authorLogin,omitempty"`
	// AuthorName is the trimmed Git author name, bounded to 256 bytes.
	AuthorName string `json:"authorName,omitempty"`
	// AuthoredAt is the Git author time in UTC.
	AuthoredAt time.Time `json:"authoredAt"`
	// CommittedAt is the Git committer time in UTC.
	CommittedAt time.Time `json:"committedAt"`
	// URL is the commit's GitHub web URL, or empty when GitHub returns an unsafe URL.
	URL string `json:"url,omitempty"`
}

// ListMergedPullRequestsOperation implements the list merged pull requests connector operation.
type ListMergedPullRequestsOperation struct{ client *Client }

// ListPullRequestFilesOperation implements the list pull request files connector operation.
type ListPullRequestFilesOperation struct{ client *Client }

// ListCommitsOperation implements the list commits connector operation.
type ListCommitsOperation struct{ client *Client }

type githubSearchIssues struct {
	TotalCount        *int                `json:"total_count"`
	IncompleteResults bool                `json:"incomplete_results"`
	Items             []githubSearchIssue `json:"items"`
}

type githubSearchIssue struct {
	Number      int                      `json:"number"`
	Title       string                   `json:"title"`
	Body        string                   `json:"body"`
	HTMLURL     string                   `json:"html_url"`
	User        *githubAccount           `json:"user"`
	Labels      []githubLabel            `json:"labels"`
	PullRequest *githubSearchPullRequest `json:"pull_request"`
}

type githubSearchPullRequest struct {
	MergedAt *time.Time `json:"merged_at"`
}

type githubAccount struct {
	Login string `json:"login"`
}

type githubLabel struct {
	Name string `json:"name"`
}

type githubPullRequestFile struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
	Changes          int    `json:"changes"`
	Patch            string `json:"patch"`
}

type githubCommit struct {
	SHA     string            `json:"sha"`
	HTMLURL string            `json:"html_url"`
	Author  *githubAccount    `json:"author"`
	Commit  githubCommitEntry `json:"commit"`
}

type githubCommitEntry struct {
	Message   string          `json:"message"`
	Author    *githubGitActor `json:"author"`
	Committer *githubGitActor `json:"committer"`
}

type githubGitActor struct {
	Name string     `json:"name"`
	Date *time.Time `json:"date"`
}

// changeBranches names the branches shared by the repository change queries.
type changeBranches struct {
	responseBranches
	invalidResponse sdkgo.BranchID
	defect          sdkgo.BranchID
}

// Definition returns the immutable connector operation definition.
func (ListMergedPullRequestsOperation) Definition() sdkgo.QueryDefinition {
	return ListMergedPullRequestsDefinition
}

// Definition returns the immutable connector operation definition.
func (ListPullRequestFilesOperation) Definition() sdkgo.QueryDefinition {
	return ListPullRequestFilesDefinition
}

// Definition returns the immutable connector operation definition.
func (ListCommitsOperation) Definition() sdkgo.QueryDefinition {
	return ListCommitsDefinition
}

// Invoke reads one GitHub search page. GitHub search cannot sort by merge time, so results are ordered
// by pull request creation time, newest first. Creation time never changes, which keeps page boundaries
// stable while later comments or edits update a pull request.
func (operation ListMergedPullRequestsOperation) Invoke(call sdkgo.Call, input ListMergedPullRequestsInput) sdkgo.QueryAttempt[MergedPullRequestPage] {
	const operationID = "listMergedPullRequests"
	branches := mergedPullRequestBranches()
	owner, repository, failure := validateRepository(operationID, input.Owner, input.Repository)
	if failure == nil {
		failure = validateWindow(operationID, "merged pull request", input.MergedAfter, input.MergedBefore)
	}
	pageSize, page := input.PageSize, input.Page
	if failure == nil {
		pageSize, page, failure = validatePage(operationID, pageSize, page, defaultMergedPullRequestPageSize)
	}
	if failure == nil && (page > searchResultLimit || (page-1)*pageSize >= searchResultLimit) {
		validationFailure := providerFailure(operationID, sdkgo.FailureValidation, "search page starts after GitHub's first 1000 results")
		failure = &validationFailure
	}
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, MergedPullRequestPage{}, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, MergedPullRequestPage{}, failure, sdkgo.Receipt{})
	}
	query := fmt.Sprintf(
		"repo:%s/%s is:pr is:merged merged:%s..%s",
		owner, repository, providerTime(input.MergedAfter), providerTime(input.MergedBefore),
	)
	response, attempt := fetchChangePage[MergedPullRequestPage](operation.client, call, credential, operationID, "/search/issues", url.Values{
		"q": {query}, "sort": {"created"}, "order": {"desc"},
		"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)},
	}, branches)
	if attempt != nil {
		return *attempt
	}
	if attempt := acceptChangeResponse[MergedPullRequestPage](operation.client, operationID, response, branches); attempt != nil {
		return *attempt
	}
	var search githubSearchIssues
	if err := decodeJSON(response.body, &search); err != nil || search.TotalCount == nil || search.Items == nil {
		return invalidChangeResponse[MergedPullRequestPage](operationID, "GitHub returned an invalid search response", response, branches)
	}
	pullRequests := make([]MergedPullRequest, 0, len(search.Items))
	for _, searchIssue := range search.Items {
		if searchIssue.Number <= 0 || searchIssue.PullRequest == nil || searchIssue.PullRequest.MergedAt == nil {
			return invalidChangeResponse[MergedPullRequestPage](operationID, "GitHub returned an invalid merged pull request", response, branches)
		}
		pullRequests = append(pullRequests, mergedPullRequestFromGitHubSearchIssue(searchIssue, operation.client.maxPullRequestBodyCharacters))
	}
	nextPage, isValidLink := nextPageNumber(response.header, page)
	if !isValidLink {
		return invalidChangeResponse[MergedPullRequestPage](operationID, "GitHub returned an invalid pagination link", response, branches)
	}
	output := MergedPullRequestPage{
		PullRequests: pullRequests, TotalCount: max(*search.TotalCount, 0),
		IncompleteResults: search.IncompleteResults, NextPage: nextPage,
	}
	return sdkgo.NewQueryBranch(ListMergedPullRequestsBranchListed, output, nil, receipt(response))
}

// Invoke reads one page of files changed by one pull request. Invalid input selects defect before
// GitHub is called.
func (operation ListPullRequestFilesOperation) Invoke(call sdkgo.Call, input ListPullRequestFilesInput) sdkgo.QueryAttempt[PullRequestFilePage] {
	const operationID = "listPullRequestFiles"
	branches := pullRequestFileBranches()
	owner, repository, failure := validateRepository(operationID, input.Owner, input.Repository)
	if failure == nil && input.Number <= 0 {
		validationFailure := providerFailure(operationID, sdkgo.FailureValidation, "pull request number must be positive")
		failure = &validationFailure
	}
	pageSize, page := input.PageSize, input.Page
	if failure == nil {
		pageSize, page, failure = validatePage(operationID, pageSize, page, defaultPullRequestFilePageSize)
	}
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, PullRequestFilePage{}, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, PullRequestFilePage{}, failure, sdkgo.Receipt{})
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/pulls/" + strconv.Itoa(input.Number) + "/files"
	response, attempt := fetchChangePage[PullRequestFilePage](operation.client, call, credential, operationID, path, url.Values{
		"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)},
	}, branches)
	if attempt != nil {
		return *attempt
	}
	if attempt := acceptChangeResponse[PullRequestFilePage](operation.client, operationID, response, branches); attempt != nil {
		return *attempt
	}
	var providerFiles []githubPullRequestFile
	if err := decodeJSON(response.body, &providerFiles); err != nil || providerFiles == nil {
		return invalidChangeResponse[PullRequestFilePage](operationID, "GitHub returned an invalid changed-file response", response, branches)
	}
	files := make([]PullRequestFile, 0, len(providerFiles))
	for _, providerFile := range providerFiles {
		file, isValidFile := pullRequestFileFromGitHubFile(providerFile, operation.client.maxPatchCharacters)
		if !isValidFile {
			return invalidChangeResponse[PullRequestFilePage](operationID, "GitHub returned an invalid changed file", response, branches)
		}
		files = append(files, file)
	}
	nextPage, isValidLink := nextPageNumber(response.header, page)
	if !isValidLink {
		return invalidChangeResponse[PullRequestFilePage](operationID, "GitHub returned an invalid pagination link", response, branches)
	}
	return sdkgo.NewQueryBranch(ListPullRequestFilesBranchListed, PullRequestFilePage{Files: files, NextPage: nextPage}, nil, receipt(response))
}

// Invoke reads one page of commits. GitHub answers 409 Conflict for a repository that has no commits
// yet. That repository has no commits in any window, so the operation selects listed with an empty
// page and records repositoryEmpty in the receipt metadata.
func (operation ListCommitsOperation) Invoke(call sdkgo.Call, input ListCommitsInput) sdkgo.QueryAttempt[CommitPage] {
	const operationID = "listCommits"
	branches := commitBranches()
	owner, repository, failure := validateRepository(operationID, input.Owner, input.Repository)
	if failure == nil {
		failure = validateWindow(operationID, "commit", input.Since, input.Until)
	}
	if failure == nil && !isValidQueryText(input.Path, maximumPathBytes, true) {
		validationFailure := providerFailure(operationID, sdkgo.FailureValidation, "commit path is invalid")
		failure = &validationFailure
	}
	if failure == nil && !isValidQueryText(input.Ref, maximumRefBytes, false) {
		validationFailure := providerFailure(operationID, sdkgo.FailureValidation, "commit ref is invalid")
		failure = &validationFailure
	}
	pageSize, page := input.PageSize, input.Page
	if failure == nil {
		pageSize, page, failure = validatePage(operationID, pageSize, page, defaultCommitPageSize)
	}
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, CommitPage{}, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, operationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, CommitPage{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{
		"since": {providerTime(input.Since)}, "until": {providerTime(input.Until)},
		"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)},
	}
	if input.Path != "" {
		query.Set("path", input.Path)
	}
	if input.Ref != "" {
		query.Set("sha", input.Ref)
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/commits"
	response, attempt := fetchChangePage[CommitPage](operation.client, call, credential, operationID, path, query, branches)
	if attempt != nil {
		return *attempt
	}
	if response.statusCode == http.StatusConflict {
		if !hasRequiredScopes(response.header) {
			return insufficientScopeResponse[CommitPage](operationID, response, branches)
		}
		emptyReceipt := receipt(response)
		emptyReceipt.Metadata = map[string]string{"repositoryEmpty": "true"}
		return sdkgo.NewQueryBranch(ListCommitsBranchListed, CommitPage{Commits: []CommitSummary{}}, nil, emptyReceipt)
	}
	if attempt := acceptChangeResponse[CommitPage](operation.client, operationID, response, branches); attempt != nil {
		return *attempt
	}
	var providerCommits []githubCommit
	if err := decodeJSON(response.body, &providerCommits); err != nil || providerCommits == nil {
		return invalidChangeResponse[CommitPage](operationID, "GitHub returned an invalid commit response", response, branches)
	}
	commits := make([]CommitSummary, 0, len(providerCommits))
	for _, providerCommit := range providerCommits {
		commit, isValidCommit := commitSummaryFromGitHubCommit(providerCommit, operation.client.maxCommitMessageCharacters)
		if !isValidCommit {
			return invalidChangeResponse[CommitPage](operationID, "GitHub returned an invalid commit", response, branches)
		}
		commits = append(commits, commit)
	}
	nextPage, isValidLink := nextPageNumber(response.header, page)
	if !isValidLink {
		return invalidChangeResponse[CommitPage](operationID, "GitHub returned an invalid pagination link", response, branches)
	}
	return sdkgo.NewQueryBranch(ListCommitsBranchListed, CommitPage{Commits: commits, NextPage: nextPage}, nil, receipt(response))
}

// fetchChangePage performs one GET. A transport failure retries; an oversized response selects invalidResponse.
func fetchChangePage[T any](
	client *Client, call sdkgo.Call, credential Credentials, operationID string, path string, query url.Values,
	branches changeBranches,
) (providerResponse, *sdkgo.QueryAttempt[T]) {
	response, err := client.get(call, &credential, path, query)
	if err == nil {
		return response, nil
	}
	var emptyOutput T
	if errors.Is(err, errResponseTooLarge) {
		failure := providerFailure(operationID, sdkgo.FailureResponseTooLarge, "GitHub response exceeds the configured size limit")
		attempt := sdkgo.NewQueryBranch(branches.invalidResponse, emptyOutput, &failure, receipt(response))
		return response, &attempt
	}
	attempt := sdkgo.NewQueryRetry[T](providerFailure(operationID, sdkgo.FailureAvailability, "GitHub is unavailable"), 0)
	return response, &attempt
}

// acceptChangeResponse classifies non-success statuses, retrying rate limits after GitHub's delay, and
// rejects a mismatched scope grant.
func acceptChangeResponse[T any](client *Client, operationID string, response providerResponse, branches changeBranches) *sdkgo.QueryAttempt[T] {
	if terminal := client.classify(operationID, response, branches.responseBranches); terminal != nil {
		var emptyOutput T
		var attempt sdkgo.QueryAttempt[T]
		if terminal.isRetryable {
			attempt = sdkgo.NewQueryRetry[T](terminal.failure, terminal.delay)
		} else {
			attempt = sdkgo.NewQueryBranch(terminal.branch, emptyOutput, &terminal.failure, receipt(response))
		}
		return &attempt
	}
	if !hasRequiredScopes(response.header) {
		attempt := insufficientScopeResponse[T](operationID, response, branches)
		return &attempt
	}
	return nil
}

func insufficientScopeResponse[T any](operationID string, response providerResponse, branches changeBranches) sdkgo.QueryAttempt[T] {
	var emptyOutput T
	failure := providerFailure(operationID, sdkgo.FailureAuthorization, "GitHub OAuth grant does not match required scopes")
	return sdkgo.NewQueryBranch(branches.insufficientScope, emptyOutput, &failure, receipt(response))
}

func invalidChangeResponse[T any](operationID string, message string, response providerResponse, branches changeBranches) sdkgo.QueryAttempt[T] {
	var emptyOutput T
	failure := providerFailure(operationID, sdkgo.FailureProtocol, message)
	return sdkgo.NewQueryBranch(branches.invalidResponse, emptyOutput, &failure, receipt(response))
}

func mergedPullRequestBranches() changeBranches {
	return changeBranches{
		responseBranches: responseBranches{
			insufficientScope: ListMergedPullRequestsBranchInsufficientScope,
			revoked:           ListMergedPullRequestsBranchAuthorizationRevoked,
			notFound:          ListMergedPullRequestsBranchNotFound,
			providerRejected:  ListMergedPullRequestsBranchProviderRejected,
		},
		invalidResponse: ListMergedPullRequestsBranchInvalidResponse,
		defect:          ListMergedPullRequestsBranchDefect,
	}
}

func pullRequestFileBranches() changeBranches {
	return changeBranches{
		responseBranches: responseBranches{
			insufficientScope: ListPullRequestFilesBranchInsufficientScope,
			revoked:           ListPullRequestFilesBranchAuthorizationRevoked,
			notFound:          ListPullRequestFilesBranchNotFound,
			providerRejected:  ListPullRequestFilesBranchProviderRejected,
		},
		invalidResponse: ListPullRequestFilesBranchInvalidResponse,
		defect:          ListPullRequestFilesBranchDefect,
	}
}

func commitBranches() changeBranches {
	return changeBranches{
		responseBranches: responseBranches{
			insufficientScope: ListCommitsBranchInsufficientScope,
			revoked:           ListCommitsBranchAuthorizationRevoked,
			notFound:          ListCommitsBranchNotFound,
			providerRejected:  ListCommitsBranchProviderRejected,
		},
		invalidResponse: ListCommitsBranchInvalidResponse,
		defect:          ListCommitsBranchDefect,
	}
}

func mergedPullRequestFromGitHubSearchIssue(searchIssue githubSearchIssue, maxBodyCharacters int) MergedPullRequest {
	body, isBodyTruncated := truncateCharacters(searchIssue.Body, maxBodyCharacters)
	labels := make([]string, 0, len(searchIssue.Labels))
	for _, label := range searchIssue.Labels {
		labels = append(labels, label.Name)
	}
	authorLogin := ""
	if searchIssue.User != nil {
		authorLogin = boundedString(searchIssue.User.Login, maximumAuthorLoginBytes)
	}
	return MergedPullRequest{
		Number: searchIssue.Number, Title: boundedString(searchIssue.Title, maximumTitleBytes),
		Body: body, BodyTruncated: isBodyTruncated, URL: safeURL(searchIssue.HTMLURL), AuthorLogin: authorLogin,
		MergedAt: searchIssue.PullRequest.MergedAt.UTC(), Labels: boundedStrings(labels, maximumLabels, maximumLabelBytes),
	}
}

func pullRequestFileFromGitHubFile(file githubPullRequestFile, maxPatchCharacters int) (PullRequestFile, bool) {
	status := boundedString(file.Status, maximumFileStatusBytes)
	if !isValidFilename(file.Filename) || status == "" || (file.PreviousFilename != "" && !isValidFilename(file.PreviousFilename)) {
		return PullRequestFile{}, false
	}
	patch, isPatchTruncated := truncateCharacters(file.Patch, maxPatchCharacters)
	return PullRequestFile{
		Filename: file.Filename, PreviousFilename: file.PreviousFilename, Status: status,
		Additions: max(file.Additions, 0), Deletions: max(file.Deletions, 0), Changes: max(file.Changes, 0),
		Patch: patch, PatchTruncated: isPatchTruncated,
	}, true
}

func commitSummaryFromGitHubCommit(commit githubCommit, maxMessageCharacters int) (CommitSummary, bool) {
	author, committer := commit.Commit.Author, commit.Commit.Committer
	if !commitSHAPattern.MatchString(commit.SHA) || author == nil || author.Date == nil || committer == nil || committer.Date == nil {
		return CommitSummary{}, false
	}
	message, isMessageTruncated := truncateCharacters(commit.Commit.Message, maxMessageCharacters)
	authorLogin := ""
	if commit.Author != nil {
		authorLogin = boundedString(commit.Author.Login, maximumAuthorLoginBytes)
	}
	return CommitSummary{
		SHA: commit.SHA, Message: message, MessageTruncated: isMessageTruncated,
		AuthorLogin: authorLogin, AuthorName: boundedString(author.Name, maximumAuthorNameBytes),
		AuthoredAt: author.Date.UTC(), CommittedAt: committer.Date.UTC(), URL: safeURL(commit.HTMLURL),
	}, true
}

func validateRepository(operationID string, owner string, repository string) (string, string, *sdkgo.Failure) {
	owner, repository = strings.TrimSpace(owner), strings.TrimSpace(repository)
	if !githubLoginPattern.MatchString(owner) {
		failure := providerFailure(operationID, sdkgo.FailureValidation, "GitHub repository owner is invalid")
		return "", "", &failure
	}
	if !repositoryNamePattern.MatchString(repository) || repository == "." || repository == ".." {
		failure := providerFailure(operationID, sdkgo.FailureValidation, "GitHub repository name is invalid")
		return "", "", &failure
	}
	return owner, repository, nil
}

func validateWindow(operationID string, subject string, windowStart time.Time, windowEnd time.Time) *sdkgo.Failure {
	if windowStart.IsZero() || windowEnd.IsZero() || !windowStart.Before(windowEnd) {
		failure := providerFailure(operationID, sdkgo.FailureValidation, subject+" window must have a start before its end")
		return &failure
	}
	return nil
}

func validatePage(operationID string, pageSize int, page int, defaultPageSize int) (int, int, *sdkgo.Failure) {
	if pageSize == 0 {
		pageSize = defaultPageSize
	}
	if page == 0 {
		page = 1
	}
	if pageSize < 1 || pageSize > providerPageSize || page < 1 {
		failure := providerFailure(operationID, sdkgo.FailureValidation, "page size must be 1 through 100 and page must be positive")
		return 0, 0, &failure
	}
	return pageSize, page, nil
}

// isValidQueryText accepts optional valid UTF-8 without control characters. A path may contain spaces; a
// Git ref cannot.
func isValidQueryText(value string, maximumBytes int, canContainSpaces bool) bool {
	if value == "" {
		return true
	}
	if len(value) > maximumBytes || !utf8.ValidString(value) {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || (!canContainSpaces && unicode.IsSpace(character))
	}) == -1
}

// providerTime formats UTC RFC 3339 seconds, which GitHub search and commits accept. Fractional seconds
// are truncated.
func providerTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}

// nextPageNumber reads the page number from the Link header's rel="next" URL. It returns zero when
// no page follows and false when the next link is malformed or does not advance.
func nextPageNumber(header http.Header, currentPage int) (int, bool) {
	for _, value := range header.Values("Link") {
		for _, match := range linkRelationPattern.FindAllStringSubmatch(value, -1) {
			if !hasLinkRelation(match[2], "next") {
				continue
			}
			target, err := url.Parse(match[1])
			if err != nil {
				return 0, false
			}
			nextPage, err := strconv.Atoi(target.Query().Get("page"))
			if err != nil || nextPage <= currentPage {
				return 0, false
			}
			return nextPage, true
		}
	}
	return 0, true
}

func hasLinkRelation(relations string, relation string) bool {
	for _, candidate := range strings.Fields(relations) {
		if strings.EqualFold(candidate, relation) {
			return true
		}
	}
	return false
}

func isValidFilename(filename string) bool {
	return filename != "" && len(filename) <= maximumFilenameBytes && utf8.ValidString(filename) && !strings.ContainsRune(filename, 0)
}

// truncateCharacters keeps at most maximumCharacters Unicode characters and reports whether any were removed.
func truncateCharacters(value string, maximumCharacters int) (string, bool) {
	characterCount := 0
	for byteOffset := range value {
		if characterCount == maximumCharacters {
			return value[:byteOffset], true
		}
		characterCount++
	}
	return value, false
}
