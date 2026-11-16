// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const (
	createIssueCommentOperationID = "createIssueComment"
	// Read-back scans the first page and the newest comment pages, which hold a recent write.
	commentReadBackPageSize  = 100
	commentReadBackPageLimit = 5
)

// CreateIssueCommentInput describes one comment to add to an existing issue.
type CreateIssueCommentInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// IssueNumber is the positive issue number within the repository.
	IssueNumber int `json:"issueNumber"`
	// Body is the nonblank Markdown comment. The connector appends a hidden HTML comment that marks the
	// comment for read-back, so Body holds at most 65536 characters less that marker.
	Body string `json:"body"`
}

// CreatedIssueComment identifies the created comment. On every other branch only IssueNumber is set,
// echoing the request so the application can reconcile.
type CreatedIssueComment struct {
	// IssueNumber echoes the requested issue number.
	IssueNumber int `json:"issueNumber"`
	// CommentID is GitHub's numeric REST ID for the comment.
	CommentID int64 `json:"commentId,omitempty"`
	// URL is the comment's GitHub web URL, or empty when GitHub returns an unsafe URL.
	URL string `json:"url,omitempty"`
	// CreatedAt is the creation time in UTC.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// CreateIssueCommentOperation implements the createIssueComment Mutation.
type CreateIssueCommentOperation struct{ client *Client }

type githubIssueComment struct {
	ID        int64          `json:"id"`
	Body      string         `json:"body"`
	HTMLURL   string         `json:"html_url"`
	User      *githubAccount `json:"user"`
	CreatedAt *time.Time     `json:"created_at"`
}

type createIssueCommentRequestBody struct {
	Body string `json:"body"`
}

// commentWriter carries one createIssueComment Invoke's validated request, credential, and call marker.
type commentWriter struct {
	client     *Client
	call       sdkgo.Call
	credential Credentials
	owner      string
	repository string
	marker     string
	requested  CreatedIssueComment
}

// commentWriteAttempt records the outcome of the single fenced POST.
type commentWriteAttempt struct {
	writer       *commentWriter
	payload      []byte
	response     providerResponse
	isDispatched bool
	// hasUnusableBody reports an answer whose body was oversized or interrupted; its status still counts.
	hasUnusableBody bool
	err             error
}

// commentReadBack is the outcome of reading back a possibly created comment.
type commentReadBack struct {
	status     issueReadBackStatus
	comment    githubIssueComment
	response   providerResponse
	failure    sdkgo.Failure
	retryAfter time.Duration
}

// Definition returns the manifest-generated execution and branch contract.
func (CreateIssueCommentOperation) Definition() sdkgo.MutationDefinition {
	return CreateIssueCommentDefinition
}

// IdempotencyKey derives the key recorded in the Receipt from the stable Call ID. GitHub accepts no
// idempotency key; the comment body carries a marker derived from the Call ID instead.
func (CreateIssueCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIssueCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one comment behind the shared single-dispatch fence. A possibly accepted earlier send is
// read back by its saved comment ID or by its body marker before another send, and the fence caps the
// sends of the Call ID at the Step's MaxSends.
func (operation CreateIssueCommentOperation) Invoke(call sdkgo.Call, input CreateIssueCommentInput) sdkgo.MutationAttempt[CreatedIssueComment] {
	writer := &commentWriter{client: operation.client, call: call, marker: issueCallMarker(call.ID)}
	request, failure := buildCreateIssueCommentRequest(input, writer)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateIssueCommentBranchDefect, writer.requested, failure, sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateIssueCommentDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return writer.branch(CreateIssueCommentBranchDefect, sdkgo.FailureLocalDefect, "GitHub dispatch fence could not be constructed", sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return writer.uncertain(sdkgo.FailureProtocol, "GitHub dispatch checkpoint cannot be trusted", sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, createIssueCommentOperationID)
	if failure == nil && credential.AuthMethodID != RepositoryAuthMethodID {
		scopeFailure := providerFailure(createIssueCommentOperationID, sdkgo.FailureAuthorization, "createIssueComment requires a connection authorized with the github-oauth-repository method")
		failure = &scopeFailure
	}
	if failure != nil {
		if decision.Kind != singledispatch.SendAllowed {
			// An earlier attempt may have created the comment, and this connection cannot read it back.
			return sdkgo.NewMutationUncertain(writer.requested, *failure, sdkgo.Receipt{})
		}
		// The branch ends the Step; the marker only lets a repeated Execute send without read-back.
		_ = fence.RecordNotSent()
		branch := CreateIssueCommentBranchDefect
		if failure.Kind == sdkgo.FailureAuthorization {
			branch = CreateIssueCommentBranchInsufficientScope
		}
		return sdkgo.NewMutationBranch(branch, writer.requested, failure, sdkgo.Receipt{})
	}
	writer.credential = credential
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		providerID := ""
		if decision.Marker.CallID == call.ID {
			providerID = decision.Marker.ProviderID
		}
		readBack := writer.findCreatedComment(providerID)
		switch {
		case readBack.status == issueReadBackFound:
			return writer.createdFromReadBack(readBack)
		case readBack.status == issueReadBackRetry:
			return sdkgo.NewMutationRetry[CreatedIssueComment](readBack.failure, readBack.retryAfter)
		case readBack.status == issueReadBackUnverifiable, decision.Kind == singledispatch.ReadBackRequired, providerID != "":
			// A saved comment ID proves an accepted send, so a missing comment is never sent again.
			return writer.uncertain(sdkgo.FailureAvailability, "GitHub comment creation outcome is unknown", receipt(readBack.response))
		}
	}
	return writer.dispatch(fence, request, decision.MaximumPriorSends+1)
}

// dispatch sends the comment once and classifies the answer. claimSendCount counts this send.
func (writer *commentWriter) dispatch(fence *singledispatch.Fence, request createIssueCommentRequestBody, claimSendCount int32) sdkgo.MutationAttempt[CreatedIssueComment] {
	payload, err := json.Marshal(request)
	if err != nil {
		return writer.branch(CreateIssueCommentBranchDefect, sdkgo.FailureLocalDefect, "GitHub comment request could not be encoded", sdkgo.Receipt{})
	}
	attempt := &commentWriteAttempt{writer: writer, payload: payload}
	if err := fence.Dispatch(attempt.send); err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return sdkgo.NewMutationRetry[CreatedIssueComment](providerFailure(createIssueCommentOperationID, sdkgo.FailureAvailability, "GitHub dispatch checkpoint or request budget is unavailable"), 0)
		}
		return writer.branch(CreateIssueCommentBranchDefect, sdkgo.FailureLocalDefect, "GitHub dispatch was not allowed", sdkgo.Receipt{})
	}
	isBelowSendCap := claimSendCount < writer.call.MaxSends()
	if attempt.err != nil {
		if !attempt.isDispatched {
			return writer.retryUnprocessed(fence, providerFailure(createIssueCommentOperationID, sdkgo.FailureTransport, "GitHub could not be reached, so no comment was created"), 0)
		}
		return writer.reconcileUnknownSend(providerFailure(createIssueCommentOperationID, sdkgo.FailureTransport, "GitHub comment creation outcome is unknown"), isBelowSendCap, sdkgo.Receipt{})
	}
	response := attempt.response
	responseReceipt := receipt(response)
	switch status := response.statusCode; {
	case status >= 200 && status < 300:
		var created githubIssueComment
		if attempt.hasUnusableBody || decodeJSON(response.body, &created) != nil || !isValidCreatedComment(created) {
			return writer.reconcileAcceptedSend(responseReceipt)
		}
		// The accepted comment ID helps after a lost Step result; failing to record it cannot authorize a resend.
		_ = fence.RecordProviderID(strconv.FormatInt(created.ID, 10))
		return writer.created(created, responseReceipt, false)
	case status == http.StatusTooManyRequests || isRateLimited(response):
		return writer.retryUnprocessed(fence, providerFailure(createIssueCommentOperationID, sdkgo.FailureRateLimit, "GitHub rate limit was reached"), retryDelay(response.header, writer.client.now()))
	case status == http.StatusServiceUnavailable:
		return writer.retryUnprocessed(fence, providerFailure(createIssueCommentOperationID, sdkgo.FailureAvailability, "GitHub is unavailable"), 0)
	case status == http.StatusUnauthorized:
		if writer.client.hasReplacementCredential(writer.call) {
			return writer.retryUnprocessed(fence, providerFailure(createIssueCommentOperationID, sdkgo.FailureAuthentication, "GitHub access token was refreshed for retry"), 0)
		}
		return writer.branch(CreateIssueCommentBranchAuthorizationRevoked, sdkgo.FailureAuthentication, "GitHub authorization is invalid or revoked", responseReceipt)
	case status == http.StatusForbidden:
		return writer.branch(CreateIssueCommentBranchInsufficientScope, sdkgo.FailureAuthorization, "GitHub authorization is forbidden or lacks scope", responseReceipt)
	case status == http.StatusNotFound:
		return writer.branch(CreateIssueCommentBranchNotFound, sdkgo.FailureNotFound, "GitHub repository or issue was not found", responseReceipt)
	case status >= 500:
		return writer.reconcileUnknownSend(providerFailure(createIssueCommentOperationID, sdkgo.FailureAvailability, "GitHub comment creation outcome is unknown after HTTP "+strconv.Itoa(status)), isBelowSendCap, responseReceipt)
	default:
		return writer.branch(CreateIssueCommentBranchProviderRejected, statusFailureKind(status), "GitHub rejected the comment with HTTP "+strconv.Itoa(status), responseReceipt)
	}
}

// send performs the single fenced POST. It never retries inside the fence.
func (attempt *commentWriteAttempt) send(requestContext context.Context) error {
	writer := attempt.writer
	observation := &dispatchObservation{}
	request, err := http.NewRequestWithContext(httptrace.WithClientTrace(requestContext, observation.clientTrace()),
		http.MethodPost, writer.client.endpoint(writer.commentsPath(), nil), bytes.NewReader(attempt.payload))
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
func (writer *commentWriter) retryUnprocessed(fence *singledispatch.Fence, failure sdkgo.Failure, delay time.Duration) sdkgo.MutationAttempt[CreatedIssueComment] {
	if err := fence.RecordCanResend(); err != nil {
		return sdkgo.NewMutationRetry[CreatedIssueComment](providerFailure(createIssueCommentOperationID, sdkgo.FailureAvailability, "GitHub retry checkpoint could not be recorded"), delay)
	}
	return sdkgo.NewMutationRetry[CreatedIssueComment](failure, delay)
}

// reconcileAcceptedSend reads back an accepted comment whose answer was unusable; it never sends again.
func (writer *commentWriter) reconcileAcceptedSend(sendReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssueComment] {
	if readBack := writer.findCreatedComment(""); readBack.status == issueReadBackFound {
		return writer.createdFromReadBack(readBack)
	}
	return writer.uncertain(sdkgo.FailureProtocol, "GitHub accepted the comment but returned an unusable comment", sendReceipt)
}

// reconcileUnknownSend reads back a possible send; below the cap a miss retries, at the cap it is uncertain.
func (writer *commentWriter) reconcileUnknownSend(failure sdkgo.Failure, isBelowSendCap bool, sendReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssueComment] {
	readBack := writer.findCreatedComment("")
	switch {
	case readBack.status == issueReadBackFound:
		return writer.createdFromReadBack(readBack)
	case readBack.status == issueReadBackRetry || isBelowSendCap:
		return sdkgo.NewMutationRetry[CreatedIssueComment](failure, readBack.retryAfter)
	default:
		return sdkgo.NewMutationUncertain(writer.requested, failure, sendReceipt)
	}
}

// findCreatedComment reads the saved comment ID, or scans the issue's first and newest comment pages for a
// comment by the connection's account that holds the marker.
func (writer *commentWriter) findCreatedComment(providerID string) commentReadBack {
	if providerID != "" {
		commentID, err := strconv.ParseInt(providerID, 10, 64)
		if err != nil || commentID <= 0 {
			return commentReadBack{status: issueReadBackUnverifiable}
		}
		readBack := writer.readCommentResponse(issuesPath(writer.owner, writer.repository)+"/comments/"+strconv.FormatInt(commentID, 10), nil)
		if readBack.status != issueReadBackFound {
			return readBack
		}
		var candidate githubIssueComment
		// The ID comes from this Call ID's accepted send, so it needs no body marker.
		if decodeJSON(readBack.response.body, &candidate) != nil || candidate.ID != commentID || !isValidCreatedComment(candidate) {
			return commentReadBack{status: issueReadBackUnverifiable, response: readBack.response}
		}
		readBack.comment = candidate
		return readBack
	}
	readBack := writer.readCommentResponse("/user", nil)
	if readBack.status != issueReadBackFound {
		return readBack
	}
	var account githubAccount
	if decodeJSON(readBack.response.body, &account) != nil || !githubLoginPattern.MatchString(account.Login) {
		return commentReadBack{status: issueReadBackUnverifiable, response: readBack.response}
	}
	// GitHub lists comments oldest first, so the newest comments sit on the last page.
	lastPage := 0
	for page, pagesRead := 1, 0; page > 0 && pagesRead < commentReadBackPageLimit; pagesRead++ {
		readBack = writer.readCommentResponse(writer.commentsPath(), url.Values{
			"per_page": {strconv.Itoa(commentReadBackPageSize)}, "page": {strconv.Itoa(page)},
		})
		if readBack.status != issueReadBackFound {
			return readBack
		}
		var candidates []githubIssueComment
		if decodeJSON(readBack.response.body, &candidates) != nil {
			return commentReadBack{status: issueReadBackUnverifiable, response: readBack.response}
		}
		for _, candidate := range candidates {
			if candidate.User != nil && strings.EqualFold(candidate.User.Login, account.Login) &&
				isValidCreatedComment(candidate) && strings.Contains(candidate.Body, writer.marker) {
				readBack.comment = candidate
				return readBack
			}
		}
		if page == 1 {
			var isValidLink bool
			lastPage, isValidLink = linkRelationPageNumber(readBack.response.header, "last")
			if !isValidLink {
				return commentReadBack{status: issueReadBackUnverifiable, response: readBack.response}
			}
			page = lastPage
		} else {
			page--
		}
		if page <= 1 {
			break
		}
	}
	return commentReadBack{status: issueReadBackNotFound, response: readBack.response}
}

// readCommentResponse performs one read-back GET. Transient failures retry; other refusals cannot verify.
func (writer *commentWriter) readCommentResponse(path string, query url.Values) commentReadBack {
	credential := writer.credential
	response, err := writer.client.get(writer.call, &credential, path, query)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return commentReadBack{status: issueReadBackUnverifiable, response: response}
		}
		return commentReadBack{status: issueReadBackRetry, failure: providerFailure(createIssueCommentOperationID, sdkgo.FailureTransport, "GitHub comment read-back is temporarily unavailable")}
	}
	if terminal := writer.client.classify(createIssueCommentOperationID, response, responseBranches{}); terminal != nil {
		if terminal.isRetryable {
			return commentReadBack{status: issueReadBackRetry, response: response, failure: terminal.failure, retryAfter: terminal.delay}
		}
		return commentReadBack{status: issueReadBackUnverifiable, response: response}
	}
	return commentReadBack{status: issueReadBackFound, response: response}
}

func (writer *commentWriter) commentsPath() string {
	return issuesPath(writer.owner, writer.repository) + "/" + strconv.Itoa(writer.requested.IssueNumber) + "/comments"
}

func (writer *commentWriter) createdFromReadBack(readBack commentReadBack) sdkgo.MutationAttempt[CreatedIssueComment] {
	return writer.created(readBack.comment, receipt(readBack.response), true)
}

func (writer *commentWriter) created(comment githubIssueComment, commentReceipt sdkgo.Receipt, isFoundByReadBack bool) sdkgo.MutationAttempt[CreatedIssueComment] {
	output := writer.requested
	output.CommentID, output.URL = comment.ID, safeURL(comment.HTMLURL)
	createdAt := comment.CreatedAt.UTC()
	output.CreatedAt = &createdAt
	commentReceipt.ProviderObjectID = strconv.FormatInt(comment.ID, 10)
	if isFoundByReadBack {
		commentReceipt.Metadata = map[string]string{"foundByReadBack": "true"}
	}
	return sdkgo.NewMutationBranch(CreateIssueCommentBranchCreated, output, nil, commentReceipt)
}

func (writer *commentWriter) branch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string, branchReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssueComment] {
	failure := providerFailure(createIssueCommentOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, writer.requested, &failure, branchReceipt)
}

func (writer *commentWriter) uncertain(kind sdkgo.FailureKind, message string, uncertainReceipt sdkgo.Receipt) sdkgo.MutationAttempt[CreatedIssueComment] {
	return sdkgo.NewMutationUncertain(writer.requested, providerFailure(createIssueCommentOperationID, kind, message), uncertainReceipt)
}

// buildCreateIssueCommentRequest validates input, sets writer's echo, and appends the call marker to the body.
func buildCreateIssueCommentRequest(input CreateIssueCommentInput, writer *commentWriter) (createIssueCommentRequestBody, *sdkgo.Failure) {
	writer.requested = CreatedIssueComment{IssueNumber: max(input.IssueNumber, 0)}
	invalid := func(message string) (createIssueCommentRequestBody, *sdkgo.Failure) {
		failure := providerFailure(createIssueCommentOperationID, sdkgo.FailureValidation, message)
		return createIssueCommentRequestBody{}, &failure
	}
	owner, repository, failure := validateRepository(createIssueCommentOperationID, input.Owner, input.Repository)
	if failure != nil {
		return createIssueCommentRequestBody{}, failure
	}
	writer.owner, writer.repository = owner, repository
	if input.IssueNumber <= 0 {
		return invalid("GitHub issue number must be positive")
	}
	if strings.TrimSpace(input.Body) == "" {
		return invalid("GitHub comment body is required")
	}
	markerSuffix := "\n\n" + writer.marker
	if !utf8.ValidString(input.Body) || strings.ContainsRune(input.Body, 0) ||
		utf8.RuneCountInString(input.Body)+utf8.RuneCountInString(markerSuffix) > maximumIssueBodyInputCharacters {
		return invalid("GitHub comment body must be valid text of at most 65536 characters including the connector marker")
	}
	return createIssueCommentRequestBody{Body: input.Body + markerSuffix}, nil
}

func isValidCreatedComment(comment githubIssueComment) bool {
	return comment.ID > 0 && comment.CreatedAt != nil && !comment.CreatedAt.IsZero()
}

// linkRelationPageNumber returns the page of a Link relation, or zero when GitHub sends none.
func linkRelationPageNumber(header http.Header, relation string) (int, bool) {
	for _, value := range header.Values("Link") {
		for _, match := range linkRelationPattern.FindAllStringSubmatch(value, -1) {
			if !hasLinkRelation(match[2], relation) {
				continue
			}
			target, err := url.Parse(match[1])
			if err != nil {
				return 0, false
			}
			page, err := strconv.Atoi(target.Query().Get("page"))
			if err != nil || page <= 0 {
				return 0, false
			}
			return page, true
		}
	}
	return 0, true
}
