// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	listIssuesOperationID       = "listIssues"
	createIssueOperationID      = "createIssue"
	defaultIssuePageSize        = 30
	defaultIssueBodyCharacters  = 4000
	maximumIssueBodyCharacters  = 16384
	maximumIssueTitleCharacters = 256
	// GitHub accepts issue bodies up to 65536 characters, including the call marker.
	maximumIssueBodyInputCharacters = 65536
	maximumIssueLabels              = 100
	maximumIssueLabelCharacters     = 50
	maximumIssueAssignees           = 10
	maximumNodeIDBytes              = 128
	maximumIssueStateBytes          = 32
	// Read-back scans the newest issues the connection's account created, which hold a recent write.
	issueReadBackPageSize  = 30
	issueReadBackPageLimit = 5
	issueCallMarkerPrefix  = "<!-- dex-connector-call: "
	issueCallMarkerSuffix  = " -->"
)

// ListIssuesInput selects one page of a repository's issues, newest created first.
type ListIssuesInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// State is open, closed, or all. Empty selects open.
	State string `json:"state,omitempty"`
	// Labels keeps only issues that carry every listed label name. A name cannot contain a comma, because
	// GitHub receives the list comma-separated.
	Labels []string `json:"labels,omitempty"`
	// Creator keeps only issues opened by this GitHub login.
	Creator string `json:"creator,omitempty"`
	// UpdatedSince keeps only issues updated at or after this time. GitHub receives it in UTC with second
	// precision.
	UpdatedSince *time.Time `json:"updatedSince,omitempty"`
	// PageSize is 1 through 100. Zero selects 30.
	PageSize int `json:"pageSize,omitempty"`
	// Page is one-based. Zero selects 1.
	Page int `json:"page,omitempty"`
	// MaxBodyCharacters bounds each issue body to 1 through 16384 Unicode characters. Zero selects 4000.
	MaxBodyCharacters int `json:"maxBodyCharacters,omitempty"`
}

// IssuePage is one bounded page of issues. NextPage is zero when no page follows.
type IssuePage struct {
	// Issues holds this page's issues, newest created first. GitHub lists pull requests with issues; they are
	// left out, so a page can hold fewer than PageSize issues while NextPage is nonzero.
	Issues []Issue `json:"issues"`
	// NextPage is the one-based page that follows, or zero when this is the last page.
	NextPage int `json:"nextPage"`
}

// Issue is one repository issue with a bounded body.
type Issue struct {
	// Number is the issue number within its repository.
	Number int `json:"number"`
	// NodeID is GitHub's global node ID for the issue, such as I_kwDOAbCdEs4AAAAM.
	NodeID string `json:"nodeId"`
	// Title is the trimmed issue title, bounded to 1024 bytes.
	Title string `json:"title"`
	// Body is the beginning of the Markdown body, bounded by ListIssuesInput.MaxBodyCharacters. It keeps the
	// hidden marker comment of an issue that createIssue wrote.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports that Body omits the end of the issue body.
	BodyTruncated bool `json:"bodyTruncated"`
	// State is open or closed.
	State string `json:"state"`
	// StateReason is GitHub's reason for the state, such as completed, not_planned, or reopened, when set.
	StateReason string `json:"stateReason,omitempty"`
	// URL is the issue's GitHub web URL, or empty when GitHub returns an unsafe URL.
	URL string `json:"url,omitempty"`
	// AuthorLogin is the login of the account that opened the issue, or empty when GitHub omits it.
	AuthorLogin string `json:"authorLogin,omitempty"`
	// Labels holds up to 100 label names, each bounded to 256 bytes.
	Labels []string `json:"labels,omitempty"`
	// Assignees holds up to 100 assignee logins.
	Assignees []string `json:"assignees,omitempty"`
	// Comments is the number of comments on the issue.
	Comments int `json:"comments"`
	// CreatedAt is the creation time in UTC.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is the last update time in UTC.
	UpdatedAt time.Time `json:"updatedAt"`
	// ClosedAt is the close time in UTC, absent for an open issue.
	ClosedAt *time.Time `json:"closedAt,omitempty"`
}

// CreateIssueInput describes one issue to create.
type CreateIssueInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// Title is the issue title, 1 through 256 Unicode characters after surrounding whitespace is trimmed,
	// without line breaks or other control characters.
	Title string `json:"title"`
	// Body is the Markdown issue body. The connector appends a hidden HTML comment that marks the issue for
	// read-back, so Body holds at most 65536 characters less that marker; empty sends only the marker.
	Body string `json:"body,omitempty"`
	// Labels names up to 100 labels of 1 through 50 characters. GitHub applies labels only for an account
	// with push access and drops them silently otherwise; CreatedIssue.Labels lists the labels applied.
	Labels []string `json:"labels,omitempty"`
	// Assignees lists up to 10 GitHub logins. GitHub applies assignees only for an account with push access
	// and drops them silently otherwise; CreatedIssue.Assignees lists the assignees applied.
	Assignees []string `json:"assignees,omitempty"`
}

// CreatedIssue identifies the created issue. On every other branch only Owner, Repository, and Title are
// set, echoing the request so the application can reconcile.
type CreatedIssue struct {
	// Owner echoes the requested repository owner.
	Owner string `json:"owner"`
	// Repository echoes the requested repository name.
	Repository string `json:"repository"`
	// Title is GitHub's title of the created issue, or the requested title on other branches.
	Title string `json:"title"`
	// Number is the issue number within its repository.
	Number int `json:"number,omitempty"`
	// ID is GitHub's numeric REST ID for the issue.
	ID int64 `json:"id,omitempty"`
	// NodeID is GitHub's global node ID for the issue.
	NodeID string `json:"nodeId,omitempty"`
	// URL is the issue's GitHub web URL, or empty when GitHub returns an unsafe URL.
	URL string `json:"url,omitempty"`
	// State is the issue state GitHub returned, normally open.
	State string `json:"state,omitempty"`
	// CreatedAt is the creation time in UTC.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// Labels lists the label names GitHub applied.
	Labels []string `json:"labels,omitempty"`
	// Assignees lists the logins GitHub assigned.
	Assignees []string `json:"assignees,omitempty"`
}

// ListIssuesOperation implements the listIssues Query.
type ListIssuesOperation struct{ client *Client }

// CreateIssueOperation implements the createIssue Mutation.
type CreateIssueOperation struct{ client *Client }

type githubIssue struct {
	ID          int64           `json:"id"`
	NodeID      string          `json:"node_id"`
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	State       string          `json:"state"`
	StateReason string          `json:"state_reason"`
	HTMLURL     string          `json:"html_url"`
	User        *githubAccount  `json:"user"`
	Labels      []githubLabel   `json:"labels"`
	Assignees   []githubAccount `json:"assignees"`
	Comments    int             `json:"comments"`
	CreatedAt   *time.Time      `json:"created_at"`
	UpdatedAt   *time.Time      `json:"updated_at"`
	ClosedAt    *time.Time      `json:"closed_at"`
	PullRequest *struct{}       `json:"pull_request"`
}

type createIssueRequestBody struct {
	Title     string   `json:"title"`
	Body      string   `json:"body"`
	Labels    []string `json:"labels,omitempty"`
	Assignees []string `json:"assignees,omitempty"`
}

// issueWriter carries one createIssue Invoke's validated request, credential, and call marker.
type issueWriter struct {
	client     *Client
	call       sdkgo.Call
	credential Credentials
	owner      string
	repository string
	marker     string
	requested  CreatedIssue
}

// issueWriteAttempt records the outcome of the single fenced POST.
type issueWriteAttempt struct {
	writer       *issueWriter
	payload      []byte
	response     providerResponse
	isDispatched bool
	// hasUnusableBody reports an answer whose body was oversized or interrupted; its status still counts.
	hasUnusableBody bool
	err             error
}

type issueReadBackStatus uint8

const (
	issueReadBackFound issueReadBackStatus = iota + 1
	issueReadBackNotFound
	issueReadBackRetry
	issueReadBackUnverifiable
)

// issueReadBack is the outcome of reading back a possibly created issue.
type issueReadBack struct {
	status     issueReadBackStatus
	issue      githubIssue
	response   providerResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// Definition returns the manifest-generated execution and branch contract.
func (ListIssuesOperation) Definition() sdkgo.QueryDefinition { return ListIssuesDefinition }

// Invoke reads one page of issues with one GitHub request. Invalid input selects defect before GitHub is
// called. The granted scopes must match the connection's authorization method exactly.
func (operation ListIssuesOperation) Invoke(call sdkgo.Call, input ListIssuesInput) sdkgo.QueryAttempt[IssuePage] {
	branches := issueListBranches()
	owner, repository, failure := validateRepository(listIssuesOperationID, input.Owner, input.Repository)
	pageSize, page := input.PageSize, input.Page
	if failure == nil {
		pageSize, page, failure = validatePage(listIssuesOperationID, pageSize, page, defaultIssuePageSize)
	}
	var query url.Values
	if failure == nil {
		query, failure = issueListQuery(input, pageSize, page)
	}
	maximumBodyCharacters := input.MaxBodyCharacters
	if maximumBodyCharacters == 0 {
		maximumBodyCharacters = defaultIssueBodyCharacters
	}
	if failure == nil && (maximumBodyCharacters < 1 || maximumBodyCharacters > maximumIssueBodyCharacters) {
		invalid := providerFailure(listIssuesOperationID, sdkgo.FailureValidation, "maxBodyCharacters must be between 1 and 16384")
		failure = &invalid
	}
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, IssuePage{}, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, listIssuesOperationID)
	if failure != nil {
		return sdkgo.NewQueryBranch(branches.defect, IssuePage{}, failure, sdkgo.Receipt{})
	}
	response, attempt := fetchChangePage[IssuePage](operation.client, call, credential, listIssuesOperationID, issuesPath(owner, repository), query, branches)
	if attempt != nil {
		return *attempt
	}
	if terminal := operation.client.classify(listIssuesOperationID, response, branches.responseBranches); terminal != nil {
		if terminal.isRetryable {
			return sdkgo.NewQueryRetry[IssuePage](terminal.failure, terminal.delay)
		}
		return sdkgo.NewQueryBranch(terminal.branch, IssuePage{}, &terminal.failure, receipt(response))
	}
	if !hasAuthMethodScopes(response.header, credential.AuthMethodID) {
		return insufficientScopeResponse[IssuePage](listIssuesOperationID, response, branches)
	}
	var providerIssues []githubIssue
	if err := decodeJSON(response.body, &providerIssues); err != nil || providerIssues == nil || len(providerIssues) > pageSize {
		return invalidChangeResponse[IssuePage](listIssuesOperationID, "GitHub returned an invalid issue page", response, branches)
	}
	issues := make([]Issue, 0, len(providerIssues))
	for _, providerIssue := range providerIssues {
		if providerIssue.PullRequest != nil {
			continue
		}
		issue, isValidIssue := issueFromGitHubIssue(providerIssue, maximumBodyCharacters)
		if !isValidIssue {
			return invalidChangeResponse[IssuePage](listIssuesOperationID, "GitHub returned an invalid issue", response, branches)
		}
		issues = append(issues, issue)
	}
	nextPage, isValidLink := nextPageNumber(response.header, page)
	if !isValidLink {
		return invalidChangeResponse[IssuePage](listIssuesOperationID, "GitHub returned an invalid pagination link", response, branches)
	}
	return sdkgo.NewQueryBranch(ListIssuesBranchListed, IssuePage{Issues: issues, NextPage: nextPage}, nil, receipt(response))
}

// Definition returns the manifest-generated execution and branch contract.
func (CreateIssueOperation) Definition() sdkgo.MutationDefinition { return CreateIssueDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable Call ID. GitHub accepts no
// idempotency key; the issue body carries a marker derived from the Call ID instead.
func (CreateIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one issue behind the shared single-dispatch fence. A possibly accepted earlier send is
// read back by its saved issue number or by its body marker before another send, and the fence caps the
// sends of the Call ID at the Step's MaxSends.
func (operation CreateIssueOperation) Invoke(call sdkgo.Call, input CreateIssueInput) sdkgo.MutationAttempt[CreatedIssue] {
	writer := &issueWriter{client: operation.client, call: call, marker: issueCallMarker(call.ID)}
	request, failure := buildCreateIssueRequest(input, writer)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateIssueBranchDefect, writer.requested, failure, sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateIssueDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return writer.branch(CreateIssueBranchDefect, sdkgo.FailureLocalDefect, "GitHub dispatch fence could not be constructed", sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return writer.uncertain(sdkgo.FailureProtocol, "GitHub dispatch checkpoint cannot be trusted", sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, createIssueOperationID)
	if failure == nil && credential.AuthMethodID != RepositoryAuthMethodID {
		scopeFailure := providerFailure(createIssueOperationID, sdkgo.FailureAuthorization, "createIssue requires a connection authorized with the github-oauth-repository method")
		failure = &scopeFailure
	}
	if failure != nil {
		if decision.Kind != singledispatch.SendAllowed {
			// An earlier attempt may have created the issue, and this connection cannot read it back.
			return sdkgo.NewMutationUncertain(writer.requested, *failure, sdkgo.Receipt{})
		}
		// The branch ends the Step; the marker only lets a repeated Execute send without read-back.
		_ = fence.RecordNotSent()
		branch := CreateIssueBranchDefect
		if failure.Kind == sdkgo.FailureAuthorization {
			branch = CreateIssueBranchInsufficientScope
		}
		return sdkgo.NewMutationBranch(branch, writer.requested, failure, sdkgo.Receipt{})
	}
	writer.credential = credential
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		providerID := ""
		if decision.Marker.CallID == call.ID {
			providerID = decision.Marker.ProviderID
		}
		readBack := writer.findCreatedIssue(providerID)
		switch {
		case readBack.status == issueReadBackFound:
			return writer.createdFromReadBack(readBack)
		case readBack.status == issueReadBackRetry:
			return sdkgo.NewMutationRetry[CreatedIssue](readBack.failure, readBack.retryAfter)
		case readBack.status == issueReadBackUnverifiable, decision.Kind == singledispatch.ReadBackRequired, providerID != "":
			// A saved issue number proves an accepted send, so a missing issue is never sent again.
			return writer.uncertain(sdkgo.FailureAvailability, "GitHub issue creation outcome is unknown", readBack.receipt())
		}
	}
	return writer.dispatch(fence, request, decision.MaximumPriorSends+1)
}

func (readBack issueReadBack) receipt() sdkgo.Receipt { return receipt(readBack.response) }

// dispatch sends the issue once and classifies the answer. claimSendCount counts this send.
func (writer *issueWriter) dispatch(fence *singledispatch.Fence, request createIssueRequestBody, claimSendCount int32) sdkgo.MutationAttempt[CreatedIssue] {
	payload, err := json.Marshal(request)
	if err != nil {
		return writer.branch(CreateIssueBranchDefect, sdkgo.FailureLocalDefect, "GitHub issue request could not be encoded", sdkgo.Receipt{})
	}
	attempt := &issueWriteAttempt{writer: writer, payload: payload}
	if err := fence.Dispatch(attempt.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[CreatedIssue](providerFailure(createIssueOperationID, sdkgo.FailureAvailability, "GitHub dispatch checkpoint or request budget is unavailable"), 0)
		}
		return writer.branch(CreateIssueBranchDefect, sdkgo.FailureLocalDefect, "GitHub dispatch was not allowed", sdkgo.Receipt{})
	}
	isBelowSendCap := claimSendCount < writer.call.MaxSends()
	if attempt.err != nil {
		if !attempt.isDispatched {
			return writer.retryUnprocessed(fence, providerFailure(createIssueOperationID, sdkgo.FailureTransport, "GitHub could not be reached, so no issue was created"), 0)
		}
		return writer.reconcileUnknownSend(providerFailure(createIssueOperationID, sdkgo.FailureTransport, "GitHub issue creation outcome is unknown"), isBelowSendCap, sdkgo.Receipt{})
	}
	response := attempt.response
	responseReceipt := receipt(response)
	switch status := response.statusCode; {
	case status >= 200 && status < 300:
		var created githubIssue
		if attempt.hasUnusableBody || decodeJSON(response.body, &created) != nil || !isValidCreatedIssue(created) {
			return writer.reconcileAcceptedSend(responseReceipt)
		}
		// The accepted issue number helps after a lost Step result; failing to record it cannot authorize a resend.
		_ = fence.RecordProviderID(strconv.Itoa(created.Number))
		return writer.created(created, responseReceipt, false)
	case status == http.StatusTooManyRequests || isRateLimited(response):
		return writer.retryUnprocessed(fence, providerFailure(createIssueOperationID, sdkgo.FailureRateLimit, "GitHub rate limit was reached"), retryDelay(response.header, writer.client.now()))
	case status == http.StatusServiceUnavailable:
		return writer.retryUnprocessed(fence, providerFailure(createIssueOperationID, sdkgo.FailureAvailability, "GitHub is unavailable"), 0)
	case status == http.StatusUnauthorized:
		if writer.client.hasReplacementCredential(writer.call) {
			return writer.retryUnprocessed(fence, providerFailure(createIssueOperationID, sdkgo.FailureAuthentication, "GitHub access token was refreshed for retry"), 0)
		}
		return writer.branch(CreateIssueBranchAuthorizationRevoked, sdkgo.FailureAuthentication, "GitHub authorization is invalid or revoked", responseReceipt)
	case status == http.StatusForbidden:
		return writer.branch(CreateIssueBranchInsufficientScope, sdkgo.FailureAuthorization, "GitHub authorization is forbidden or lacks scope", responseReceipt)
	case status == http.StatusNotFound:
		return writer.branch(CreateIssueBranchNotFound, sdkgo.FailureNotFound, "GitHub repository was not found", responseReceipt)
	case status >= 500:
		return writer.reconcileUnknownSend(providerFailure(createIssueOperationID, sdkgo.FailureAvailability, "GitHub issue creation outcome is unknown after HTTP "+strconv.Itoa(status)), isBelowSendCap, responseReceipt)
	default:
		return writer.branch(CreateIssueBranchProviderRejected, statusFailureKind(status), "GitHub rejected the issue with HTTP "+strconv.Itoa(status), responseReceipt)
	}
}

// send performs the single fenced POST. It never retries inside the fence.
func (attempt *issueWriteAttempt) send(requestContext context.Context) error {
	writer := attempt.writer
	observation := &dispatchObservation{}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(requestContext, observation.clientTrace()),
		http.MethodPost, writer.client.endpoint(issuesPath(writer.owner, writer.repository), nil), bytes.NewReader(attempt.payload))
	if err != nil {
		attempt.err = err
		return nil
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+writer.credential.AccessToken.Reveal())
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("X-GitHub-Api-Version", writer.client.apiVersion)
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		attempt.err = err
		return nil
	}
	response, err := writer.client.httpClient.Do(request)
	if err != nil {
		attempt.err, attempt.isDispatched = err, !observation.isProvablyUndispatched()
		return nil
	}
	attempt.isDispatched = true
	body, readErr := io.ReadAll(io.LimitReader(response.Body, writer.client.maxResponseBytes+1))
	closeErr := response.Body.Close()
	attempt.response = providerResponse{statusCode: response.StatusCode, header: response.Header.Clone(), requestID: safeRequestID(response.Header)}
	if readErr != nil || closeErr != nil || int64(len(body)) > writer.client.maxResponseBytes {
		attempt.hasUnusableBody = true
		return nil
	}
	attempt.response.body = body
	return nil
}

// retryUnprocessed records positive evidence that GitHub did not process the send, then returns Retry.
func (writer *issueWriter) retryUnprocessed(fence *singledispatch.Fence, failure sdkgo.Failure, delay time.Duration) sdkgo.MutationAttempt[CreatedIssue] {
	if err := fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[CreatedIssue](providerFailure(createIssueOperationID, sdkgo.FailureAvailability, "GitHub retry checkpoint could not be recorded"), delay)
	}
	return sdkgo.NewMutationRetry[CreatedIssue](failure, delay)
}

// reconcileAcceptedSend reads back an accepted issue whose answer was unusable; it never sends again.
func (writer *issueWriter) reconcileAcceptedSend(sendReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssue] {
	if readBack := writer.findCreatedIssue(""); readBack.status == issueReadBackFound {
		return writer.createdFromReadBack(readBack)
	}
	return writer.uncertain(sdkgo.FailureProtocol, "GitHub accepted the issue but returned an unusable issue", sendReceipt)
}

// reconcileUnknownSend reads back a possible send; below the cap a miss retries, at the cap it is uncertain.
func (writer *issueWriter) reconcileUnknownSend(failure sdkgo.Failure, isBelowSendCap bool, sendReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssue] {
	readBack := writer.findCreatedIssue("")
	switch {
	case readBack.status == issueReadBackFound:
		return writer.createdFromReadBack(readBack)
	case readBack.status == issueReadBackRetry || isBelowSendCap:
		return sdkgo.NewMutationRetry[CreatedIssue](failure, readBack.retryAfter)
	default:
		return sdkgo.NewMutationUncertain(writer.requested, failure, sendReceipt)
	}
}

// findCreatedIssue reads the saved issue number, or scans the account's newest issues for the marker.
func (writer *issueWriter) findCreatedIssue(providerID string) issueReadBack {
	if providerID != "" {
		number, err := strconv.Atoi(providerID)
		if err != nil || number <= 0 {
			return issueReadBack{status: issueReadBackUnverifiable}
		}
		readBack := writer.readIssueResponse(issuesPath(writer.owner, writer.repository)+"/"+strconv.Itoa(number), nil)
		if readBack.status != issueReadBackFound {
			return readBack
		}
		var candidate githubIssue
		// The number comes from this Call ID's accepted send, so it needs no body marker.
		if decodeJSON(readBack.response.body, &candidate) != nil || candidate.Number != number || !isValidCreatedIssue(candidate) {
			return issueReadBack{status: issueReadBackUnverifiable, response: readBack.response}
		}
		readBack.issue = candidate
		return readBack
	}
	readBack := writer.readIssueResponse("/user", nil)
	if readBack.status != issueReadBackFound {
		return readBack
	}
	var account githubAccount
	if decodeJSON(readBack.response.body, &account) != nil || !githubLoginPattern.MatchString(account.Login) {
		return issueReadBack{status: issueReadBackUnverifiable, response: readBack.response}
	}
	for page := 1; page <= issueReadBackPageLimit; page++ {
		readBack = writer.readIssueResponse(issuesPath(writer.owner, writer.repository), url.Values{
			"creator": {account.Login}, "state": {"all"}, "sort": {"created"}, "direction": {"desc"},
			"per_page": {strconv.Itoa(issueReadBackPageSize)}, "page": {strconv.Itoa(page)},
		})
		if readBack.status != issueReadBackFound {
			return readBack
		}
		var candidates []githubIssue
		if decodeJSON(readBack.response.body, &candidates) != nil {
			return issueReadBack{status: issueReadBackUnverifiable, response: readBack.response}
		}
		for _, candidate := range candidates {
			if candidate.PullRequest == nil && candidate.User != nil && strings.EqualFold(candidate.User.Login, account.Login) &&
				isValidCreatedIssue(candidate) && strings.Contains(candidate.Body, writer.marker) {
				readBack.issue = candidate
				return readBack
			}
		}
		nextPage, isValidLink := nextPageNumber(readBack.response.header, page)
		if !isValidLink {
			return issueReadBack{status: issueReadBackUnverifiable, response: readBack.response}
		}
		if nextPage == 0 {
			break
		}
	}
	return issueReadBack{status: issueReadBackNotFound, response: readBack.response}
}

// readIssueResponse performs one read-back GET. Transient failures retry; other refusals cannot verify.
func (writer *issueWriter) readIssueResponse(path string, query url.Values) issueReadBack {
	credential := writer.credential
	response, err := writer.client.get(writer.call, &credential, path, query)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return issueReadBack{status: issueReadBackUnverifiable, response: response}
		}
		return issueReadBack{status: issueReadBackRetry, failure: providerFailure(createIssueOperationID, sdkgo.FailureTransport, "GitHub issue read-back is temporarily unavailable")}
	}
	if terminal := writer.client.classify(createIssueOperationID, response, responseBranches{}); terminal != nil {
		if terminal.isRetryable {
			return issueReadBack{status: issueReadBackRetry, response: response, failure: terminal.failure, retryAfter: terminal.delay}
		}
		return issueReadBack{status: issueReadBackUnverifiable, response: response}
	}
	return issueReadBack{status: issueReadBackFound, response: response}
}

func (writer *issueWriter) createdFromReadBack(readBack issueReadBack) sdkgo.MutationAttempt[CreatedIssue] {
	return writer.created(readBack.issue, readBack.receipt(), true)
}

func (writer *issueWriter) created(issue githubIssue, issueReceipt sdkgo.Receipt, isFoundByReadBack bool) sdkgo.MutationAttempt[CreatedIssue] {
	output := writer.requested
	output.Title = boundedString(issue.Title, maximumTitleBytes)
	output.Number, output.ID, output.NodeID = issue.Number, max(issue.ID, 0), issue.NodeID
	output.URL, output.State = safeURL(issue.HTMLURL), boundedString(issue.State, maximumIssueStateBytes)
	createdAt := issue.CreatedAt.UTC()
	output.CreatedAt = &createdAt
	output.Labels, output.Assignees = issueLabelNames(issue), issueAssigneeLogins(issue)
	issueReceipt.ProviderObjectID = issue.NodeID
	if isFoundByReadBack {
		issueReceipt.Metadata = map[string]string{"foundByReadBack": "true"}
	}
	return sdkgo.NewMutationBranch(CreateIssueBranchCreated, output, nil, issueReceipt)
}

func (writer *issueWriter) branch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, branchReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssue] {
	failure := providerFailure(createIssueOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, writer.requested, &failure, branchReceipt)
}

func (writer *issueWriter) uncertain(kind sdkgo.FailureKind, message string, uncertainReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssue] {
	return sdkgo.NewMutationUncertain(writer.requested, providerFailure(createIssueOperationID, kind, message), uncertainReceipt)
}

// hasReplacementCredential reports whether the credential source issued a new token after a 401.
func (client *Client) hasReplacementCredential(call sdkgo.Call) bool {
	if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
		return false
	}
	replacement, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
	return err == nil && validateResolvedCredentials(replacement) == nil
}

// endpoint joins an API path and query to the configured base URL.
func (client *Client) endpoint(path string, query url.Values) string {
	target := *client.baseURL
	target.Path = strings.TrimRight(target.Path, "/") + path
	target.RawPath = ""
	target.RawQuery = query.Encode()
	return target.String()
}

// buildCreateIssueRequest validates input, sets writer's echo, and appends the call marker to the body.
func buildCreateIssueRequest(input CreateIssueInput, writer *issueWriter) (createIssueRequestBody, *sdkgo.Failure) {
	writer.requested = CreatedIssue{Owner: strings.TrimSpace(input.Owner), Repository: strings.TrimSpace(input.Repository), Title: strings.TrimSpace(input.Title)}
	invalid := func(message string) (createIssueRequestBody, *sdkgo.Failure) {
		failure := providerFailure(createIssueOperationID, sdkgo.FailureValidation, message)
		return createIssueRequestBody{}, &failure
	}
	owner, repository, failure := validateRepository(createIssueOperationID, input.Owner, input.Repository)
	if failure != nil {
		return createIssueRequestBody{}, failure
	}
	writer.owner, writer.repository = owner, repository
	title := writer.requested.Title
	switch {
	case title == "":
		return invalid("GitHub issue title is required")
	case !isSingleLineText(title) || utf8.RuneCountInString(title) > maximumIssueTitleCharacters:
		return invalid("GitHub issue title must be one line of at most 256 characters")
	}
	markerSuffix := "\n\n" + writer.marker
	if !utf8.ValidString(input.Body) || strings.ContainsRune(input.Body, 0) ||
		utf8.RuneCountInString(input.Body)+utf8.RuneCountInString(markerSuffix) > maximumIssueBodyInputCharacters {
		return invalid("GitHub issue body must be valid text of at most 65536 characters including the connector marker")
	}
	body := writer.marker
	if strings.TrimSpace(input.Body) != "" {
		body = input.Body + markerSuffix
	}
	if len(input.Labels) > maximumIssueLabels {
		return invalid("GitHub issue has more than 100 labels")
	}
	labels := make([]string, 0, len(input.Labels))
	for _, label := range input.Labels {
		label = strings.TrimSpace(label)
		if label == "" || !isSingleLineText(label) || utf8.RuneCountInString(label) > maximumIssueLabelCharacters {
			return invalid("GitHub issue labels must be one line of 1 through 50 characters")
		}
		labels = append(labels, label)
	}
	if len(input.Assignees) > maximumIssueAssignees {
		return invalid("GitHub issue has more than 10 assignees")
	}
	assignees := make([]string, 0, len(input.Assignees))
	for _, assignee := range input.Assignees {
		assignee = strings.TrimSpace(assignee)
		if !githubLoginPattern.MatchString(assignee) {
			return invalid("GitHub issue assignee login is invalid")
		}
		assignees = append(assignees, assignee)
	}
	return createIssueRequestBody{Title: title, Body: body, Labels: labels, Assignees: assignees}, nil
}

// issueListQuery validates the listIssues filters and returns GitHub's query, newest created first.
func issueListQuery(input ListIssuesInput, pageSize int, page int) (url.Values, *sdkgo.Failure) {
	invalid := func(message string) (url.Values, *sdkgo.Failure) {
		failure := providerFailure(listIssuesOperationID, sdkgo.FailureValidation, message)
		return nil, &failure
	}
	state := strings.TrimSpace(input.State)
	switch state {
	case "":
		state = "open"
	case "open", "closed", "all":
	default:
		return invalid("issue state must be open, closed, or all")
	}
	query := url.Values{
		"state": {state}, "sort": {"created"}, "direction": {"desc"},
		"per_page": {strconv.Itoa(pageSize)}, "page": {strconv.Itoa(page)},
	}
	if len(input.Labels) > maximumLabels {
		return invalid("issue label filter has more than 100 labels")
	}
	labels := make([]string, 0, len(input.Labels))
	for _, label := range input.Labels {
		label = strings.TrimSpace(label)
		if label == "" || len(label) > maximumLabelBytes || !isSingleLineText(label) || strings.Contains(label, ",") {
			return invalid("issue label filter names must be one line without commas")
		}
		labels = append(labels, label)
	}
	if len(labels) > 0 {
		query.Set("labels", strings.Join(labels, ","))
	}
	if creator := strings.TrimSpace(input.Creator); creator != "" {
		if !githubLoginPattern.MatchString(creator) {
			return invalid("issue creator login is invalid")
		}
		query.Set("creator", creator)
	}
	if input.UpdatedSince != nil {
		if input.UpdatedSince.IsZero() {
			return invalid("issue updatedSince must be a time")
		}
		query.Set("since", providerTime(*input.UpdatedSince))
	}
	return query, nil
}

func issueFromGitHubIssue(providerIssue githubIssue, maximumBodyCharacters int) (Issue, bool) {
	state := boundedString(providerIssue.State, maximumIssueStateBytes)
	if providerIssue.Number <= 0 || state == "" || providerIssue.CreatedAt == nil || providerIssue.UpdatedAt == nil ||
		len(providerIssue.NodeID) > maximumNodeIDBytes {
		return Issue{}, false
	}
	body, isBodyTruncated := truncateCharacters(providerIssue.Body, maximumBodyCharacters)
	authorLogin := ""
	if providerIssue.User != nil {
		authorLogin = boundedString(providerIssue.User.Login, maximumAuthorLoginBytes)
	}
	issue := Issue{
		Number: providerIssue.Number, NodeID: providerIssue.NodeID, Title: boundedString(providerIssue.Title, maximumTitleBytes),
		Body: body, BodyTruncated: isBodyTruncated, State: state, StateReason: boundedString(providerIssue.StateReason, maximumIssueStateBytes),
		URL: safeURL(providerIssue.HTMLURL), AuthorLogin: authorLogin, Labels: issueLabelNames(providerIssue),
		Assignees: issueAssigneeLogins(providerIssue), Comments: max(providerIssue.Comments, 0),
		CreatedAt: providerIssue.CreatedAt.UTC(), UpdatedAt: providerIssue.UpdatedAt.UTC(),
	}
	if providerIssue.ClosedAt != nil {
		closedAt := providerIssue.ClosedAt.UTC()
		issue.ClosedAt = &closedAt
	}
	return issue, true
}

func isValidCreatedIssue(issue githubIssue) bool {
	return issue.Number > 0 && issue.NodeID != "" && len(issue.NodeID) <= maximumNodeIDBytes &&
		strings.TrimSpace(issue.State) != "" && issue.CreatedAt != nil && !issue.CreatedAt.IsZero()
}

func issueLabelNames(issue githubIssue) []string {
	labels := make([]string, 0, len(issue.Labels))
	for _, label := range issue.Labels {
		labels = append(labels, label.Name)
	}
	return boundedStrings(labels, maximumLabels, maximumLabelBytes)
}

func issueAssigneeLogins(issue githubIssue) []string {
	logins := make([]string, 0, len(issue.Assignees))
	for _, assignee := range issue.Assignees {
		logins = append(logins, assignee.Login)
	}
	return boundedStrings(logins, maximumLabels, maximumAuthorLoginBytes)
}

func issueListBranches() changeBranches {
	return changeBranches{
		responseBranches: responseBranches{
			insufficientScope: ListIssuesBranchInsufficientScope,
			revoked:           ListIssuesBranchAuthorizationRevoked,
			notFound:          ListIssuesBranchNotFound,
			providerRejected:  ListIssuesBranchProviderRejected,
		},
		invalidResponse: ListIssuesBranchInvalidResponse,
		defect:          ListIssuesBranchDefect,
	}
}

// hasAuthMethodScopes reports whether GitHub granted exactly the scopes of the connection's method.
func hasAuthMethodScopes(header http.Header, authMethodID string) bool {
	scopes, isKnownMethod := requiredOAuthScopes(authMethodID)
	return isKnownMethod && hasExactGrantedScopes(header, scopes)
}

// issueCallMarker is the hidden Markdown comment holding a digest of the Call ID, never Flow input.
func issueCallMarker(callID sdkgo.CallID) string {
	digest := sha256.Sum256([]byte(callID))
	return issueCallMarkerPrefix + hex.EncodeToString(digest[:]) + issueCallMarkerSuffix
}

func issuesPath(owner string, repository string) string {
	return "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repository) + "/issues"
}

func isSingleLineText(value string) bool {
	return utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) == -1
}

func statusFailureKind(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return sdkgo.FailureValidation
	default:
		return sdkgo.FailureProviderRejection
	}
}

// dispatchObservation traces whether a failed request provably never reached GitHub.
type dispatchObservation struct {
	mutex                 sync.Mutex
	hasFailedToConnect    bool
	hasObtainedConnection bool
}

func (observation *dispatchObservation) clientTrace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		DNSDone:          observation.recordDNSDone,
		ConnectDone:      observation.recordConnectDone,
		TLSHandshakeDone: observation.recordTLSHandshakeDone,
		GotConn:          observation.recordObtainedConnection,
	}
}

// isProvablyUndispatched requires a traced DNS, connect, or TLS failure and no obtained connection.
func (observation *dispatchObservation) isProvablyUndispatched() bool {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	return observation.hasFailedToConnect && !observation.hasObtainedConnection
}

func (observation *dispatchObservation) recordDNSDone(info httptrace.DNSDoneInfo) {
	if info.Err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordConnectDone(_ string, _ string, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordTLSHandshakeDone(_ tls.ConnectionState, err error) {
	if err != nil {
		observation.recordConnectionFailure()
	}
}

func (observation *dispatchObservation) recordObtainedConnection(httptrace.GotConnInfo) {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasObtainedConnection = true
}

func (observation *dispatchObservation) recordConnectionFailure() {
	observation.mutex.Lock()
	defer observation.mutex.Unlock()
	observation.hasFailedToConnect = true
}
