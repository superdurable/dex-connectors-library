// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateIssueOperation creates a project issue with bounded resends.
type CreateIssueOperation struct{ client *Client }

// CreateIssueCommentOperation creates an issue note with bounded resends.
type CreateIssueCommentOperation struct{ client *Client }

// CreateIssue binds the issue creation Mutation to this client.
func (client *Client) CreateIssue() CreateIssueOperation { return CreateIssueOperation{client} }

// CreateIssueComment binds the note creation Mutation to this client.
func (client *Client) CreateIssueComment() CreateIssueCommentOperation {
	return CreateIssueCommentOperation{client}
}

// Definition returns the generated createIssue contract.
func (CreateIssueOperation) Definition() sdkgo.MutationDefinition { return CreateIssueDefinition }

// Definition returns the generated createIssueComment contract.
func (CreateIssueCommentOperation) Definition() sdkgo.MutationDefinition {
	return CreateIssueCommentDefinition
}

// IdempotencyKey derives a stable retry identity from the Dex call.
func (CreateIssueOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIssueInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives a stable retry identity from the Dex call.
func (CreateIssueCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateIssueCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an issue or reports a bounded unknown outcome.
func (operation CreateIssueOperation) Invoke(call sdkgo.Call, input CreateIssueInput) sdkgo.MutationAttempt[Issue] {
	if strings.TrimSpace(input.ProjectID) == "" || strings.TrimSpace(input.Title) == "" || len(input.Title) > 255 || len(input.Description) > 1048576 {
		return limitedBranch(CreateIssueBranchDefect, Issue{}, "createIssue", sdkgo.FailureValidation, "project ID, title, or description is invalid")
	}
	form := url.Values{"title": {input.Title}}
	if input.Description != "" {
		form.Set("description", input.Description)
	}
	projectPath := "/projects/" + url.PathEscape(input.ProjectID) + "/issues"
	request := limitedWrite[Issue]{client: operation.client, operation: "createIssue", spec: requestSpec{method: http.MethodPost, path: projectPath, body: []byte(form.Encode()), contentType: "application/x-www-form-urlencoded"}, created: CreateIssueBranchCreated, rejected: CreateIssueBranchProviderRejected, defect: CreateIssueBranchDefect, decode: decodeIssue, providerID: func(issue Issue) string { return strconv.FormatInt(issue.IID, 10) }, readBackPath: func(id string) string { return projectPath + "/" + id }}
	return request.invoke(call)
}

// Invoke creates an issue note or reports a bounded unknown outcome.
func (operation CreateIssueCommentOperation) Invoke(call sdkgo.Call, input CreateIssueCommentInput) sdkgo.MutationAttempt[IssueComment] {
	path, err := issuePath(GetIssueInput{ProjectID: input.ProjectID, IssueIID: input.IssueIID})
	if err != nil || strings.TrimSpace(input.Body) == "" || len(input.Body) > 1000000 {
		return limitedBranch(CreateIssueCommentBranchDefect, IssueComment{}, "createIssueComment", sdkgo.FailureValidation, "project, issue IID, or comment body is invalid")
	}
	path += "/notes"
	form := url.Values{"body": {input.Body}}
	request := limitedWrite[IssueComment]{client: operation.client, operation: "createIssueComment", spec: requestSpec{method: http.MethodPost, path: path, body: []byte(form.Encode()), contentType: "application/x-www-form-urlencoded"}, created: CreateIssueCommentBranchCreated, rejected: CreateIssueCommentBranchProviderRejected, defect: CreateIssueCommentBranchDefect, decode: decodeIssueComment, providerID: func(comment IssueComment) string { return strconv.FormatInt(comment.ID, 10) }, readBackPath: func(id string) string { return path + "/" + id }}
	return request.invoke(call)
}

type limitedWrite[T any] struct {
	client            *Client
	operation         string
	spec              requestSpec
	created           sdkgo.BranchID
	rejected          sdkgo.BranchID
	defect            sdkgo.BranchID
	decode            func([]byte) (T, error)
	providerID        func(T) string
	readBackPath      func(string) string
	naturalReadBackID string
	matchesReadBack   func(T) bool
	token             string
	status            int
	body              []byte
	retryAfter        time.Duration
}

func (request *limitedWrite[T]) invoke(call sdkgo.Call) sdkgo.MutationAttempt[T] {
	var zero T
	fence, err := singledispatch.NewFence(call, 30*time.Second, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return limitedBranch(request.defect, zero, request.operation, sdkgo.FailureLocalDefect, "dispatch fence is unavailable")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return request.uncertain(call, "an earlier write may have succeeded")
	}
	if decision.Kind == singledispatch.ReadBackRequired || decision.Kind == singledispatch.ReadBackThenResendAllowed {
		readBackID := decision.Marker.ProviderID
		if readBackID == "" {
			readBackID = request.naturalReadBackID
		}
		if readBackID != "" {
			value, found, readErr := request.readBack(call, readBackID)
			if readErr != nil {
				return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: request.operation, Message: "GitLab read-back is unavailable"}, 0)
			}
			if found && request.matchesReadBack != nil && !request.matchesReadBack(value) {
				return request.uncertain(call, "read-back found a different provider resource")
			}
			if found {
				return sdkgo.NewMutationBranch(request.created, value, nil, request.receipt(call, request.providerID(value)))
			}
		}
		if decision.Kind == singledispatch.ReadBackRequired {
			return request.uncertain(call, "an earlier write cannot be confirmed")
		}
	}
	credentials, err := request.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		if decision.MaximumPriorSends > 0 {
			return request.uncertain(call, "an earlier write may have succeeded; credentials are unavailable")
		}
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: request.operation, Message: "credential checkpoint is unavailable"}, 0)
		}
		return limitedBranch(request.defect, zero, request.operation, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request.token = credentials.AccessToken.Reveal()
	err = fence.Dispatch(request.send)
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: request.operation, Message: "write was not dispatched"}, 0)
	}
	if request.status == http.StatusTooManyRequests || request.status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: request.operation, Message: "retry checkpoint is unavailable"}, 0)
		}
		kind := sdkgo.FailureRateLimit
		if request.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: kind, Provider: "gitlab", Operation: request.operation, Message: "GitLab asked to retry the write"}, request.retryAfter)
	}
	if err != nil || request.status >= 500 || request.status == http.StatusRequestTimeout {
		return request.retryUnknown(call, decision.MaximumPriorSends, "write outcome is unknown")
	}
	if request.status == http.StatusCreated {
		value, decodeErr := request.decode(request.body)
		if decodeErr != nil {
			return request.uncertain(call, "GitLab accepted a write but returned invalid JSON")
		}
		id := request.providerID(value)
		// The accepted result is valid even if the optional recovery checkpoint fails.
		_ = fence.RecordProviderID(id)
		return sdkgo.NewMutationBranch(request.created, value, nil, request.receipt(call, id))
	}
	if decision.MaximumPriorSends > 0 {
		return request.uncertain(call, "a previous write may have succeeded")
	}
	return limitedBranch(request.rejected, zero, request.operation, sdkgo.FailureProviderRejection, "GitLab rejected HTTP "+strconv.Itoa(request.status))
}

func (request *limitedWrite[T]) send(ctx context.Context) error {
	var err error
	request.body, request.status, request.retryAfter, err = request.client.sendLimited(ctx, request.token, request.spec)
	return err
}

func (request *limitedWrite[T]) readBack(call sdkgo.Call, id string) (T, bool, error) {
	var zero T
	response, localFailure, err := request.client.send(call, request.operation, requestSpec{method: http.MethodGet, path: request.readBackPath(id)})
	if localFailure != nil || err != nil {
		return zero, false, errors.New("read-back failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return zero, false, nil
	}
	if response.StatusCode != http.StatusOK {
		return zero, false, errors.New("read-back rejected")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, request.client.maxResponseBytes)
	if err != nil {
		return zero, false, err
	}
	value, err := request.decode(body)
	if err != nil {
		return zero, false, err
	}
	return value, true, nil
}

func (request *limitedWrite[T]) retryUnknown(call sdkgo.Call, priorSends int32, message string) sdkgo.MutationAttempt[T] {
	if priorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[T](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "gitlab", Operation: request.operation, Message: message}, request.retryAfter)
	}
	return request.uncertain(call, message+" after the send limit")
}

func (request *limitedWrite[T]) uncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[T] {
	var zero T
	return sdkgo.NewMutationUncertain(zero, sdkgo.Failure{Kind: sdkgo.FailureTransport, Provider: "gitlab", Operation: request.operation, Message: message}, request.receipt(call, ""))
}

func (request *limitedWrite[T]) receipt(call sdkgo.Call, id string) sdkgo.Receipt {
	return sdkgo.Receipt{CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "gitlab", ProviderObjectID: id}
}

func limitedBranch[T any](branch sdkgo.BranchID, value T, operation string, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	return sdkgo.NewMutationBranch(branch, value, failure(kind, operation, message), sdkgo.Receipt{})
}

func (client *Client) sendLimited(ctx context.Context, token string, spec requestSpec) ([]byte, int, time.Duration, error) {
	endpoint := client.baseURL + spec.path
	if len(spec.query) > 0 {
		endpoint += "?" + spec.query.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, spec.method, endpoint, bytes.NewReader(spec.body))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("build GitLab request: %w", err)
	}
	request.Header.Set("PRIVATE-TOKEN", token)
	request.Header.Set("Accept", "application/json")
	if spec.contentType != "" {
		request.Header.Set("Content-Type", spec.contentType)
	}
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return nil, 0, 0, err
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return body, response.StatusCode, providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err
}

func decodeIssueComment(body []byte) (IssueComment, error) {
	var comment IssueComment
	if err := json.Unmarshal(body, &comment); err != nil {
		return IssueComment{}, err
	}
	if comment.ID <= 0 {
		return IssueComment{}, fmt.Errorf("missing issue note ID")
	}
	return comment, nil
}
