// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	// RepositoryPushEventKey identifies a repository push webhook.
	RepositoryPushEventKey = "repo:push"
	// PullRequestCreatedEventKey identifies a newly created pull request.
	PullRequestCreatedEventKey = "pullrequest:created"
	// PullRequestUpdatedEventKey identifies an updated pull request.
	PullRequestUpdatedEventKey = "pullrequest:updated"
	// PullRequestMergedEventKey identifies a merged pull request.
	PullRequestMergedEventKey = "pullrequest:fulfilled"
	// PullRequestDeclinedEventKey identifies a declined pull request.
	PullRequestDeclinedEventKey = "pullrequest:rejected"
	// PullRequestChangesRequestedEventKey identifies a review change request.
	PullRequestChangesRequestedEventKey = "pullrequest:changes_request_created"
	// PullRequestApprovedEventKey identifies a pull request approval.
	PullRequestApprovedEventKey = "pullrequest:approved"
	// PullRequestUnapprovedEventKey identifies a removed approval.
	PullRequestUnapprovedEventKey = "pullrequest:unapproved"
	// PullRequestCommentCreatedEventKey identifies a newly created comment.
	PullRequestCommentCreatedEventKey = "pullrequest:comment_created"
	// PullRequestCommentUpdatedEventKey identifies an updated comment.
	PullRequestCommentUpdatedEventKey = "pullrequest:comment_updated"
	// PullRequestCommentDeletedEventKey identifies a deleted comment.
	PullRequestCommentDeletedEventKey = "pullrequest:comment_deleted"
	// PullRequestCommentResolvedEventKey identifies a resolved comment.
	PullRequestCommentResolvedEventKey = "pullrequest:comment_resolved"
	// PullRequestCommentReopenedEventKey identifies a reopened comment.
	PullRequestCommentReopenedEventKey = "pullrequest:comment_reopened"
)

var repositoryEventKeys = []string{
	RepositoryPushEventKey, "repo:fork", "repo:updated", "repo:commit_comment_created", "repo:commit_status_created", "repo:commit_status_updated",
	PullRequestCreatedEventKey, "pullrequest:push", PullRequestUpdatedEventKey, PullRequestMergedEventKey, PullRequestDeclinedEventKey,
	PullRequestChangesRequestedEventKey, "pullrequest:changes_request_removed", PullRequestApprovedEventKey, PullRequestUnapprovedEventKey,
	PullRequestCommentCreatedEventKey, PullRequestCommentUpdatedEventKey, PullRequestCommentDeletedEventKey, PullRequestCommentResolvedEventKey, PullRequestCommentReopenedEventKey,
	"issue:created", "issue:updated", "issue:comment_created", "pipeline:span_created",
	"project:updated", "repo:created", "repo:deleted", "repo:imported", "repo:transfer",
}

var errRepositoryEventSignatureInvalid = errors.New("Bitbucket webhook signature is invalid")
var errRepositoryEventMalformed = errors.New("Bitbucket webhook event is malformed")

// RepositoryEventReceivedTriggerConfiguration limits accepted Bitbucket event keys.
// An empty list accepts every supported event key.
type RepositoryEventReceivedTriggerConfiguration struct {
	// EventKeys contains distinct supported event keys; empty accepts all.
	EventKeys []string `json:"eventKeys,omitempty"`
}

// RepositoryPushChange reports one old and new Git ref target in a push.
type RepositoryPushChange struct {
	// OldRefType identifies a branch or tag before the push.
	OldRefType string `json:"oldRefType,omitempty"`
	// OldBranch is the branch before the push, if any.
	OldBranch string `json:"oldBranch,omitempty"`
	// OldTag is the tag before the push, if any.
	OldTag string `json:"oldTag,omitempty"`
	// OldTargetHash is the old commit hash, if any.
	OldTargetHash string `json:"oldTargetHash,omitempty"`
	// NewRefType identifies a branch or tag after the push.
	NewRefType string `json:"newRefType,omitempty"`
	// NewBranch is the branch after the push, if any.
	NewBranch string `json:"newBranch,omitempty"`
	// NewTag is the tag after the push, if any.
	NewTag string `json:"newTag,omitempty"`
	// NewTargetHash is the new commit hash, if any.
	NewTargetHash string `json:"newTargetHash,omitempty"`
}

// RepositoryEvent is one HMAC-verified Bitbucket repository or workspace webhook event.
type RepositoryEvent struct {
	// EventKey is one of the supported Bitbucket event keys.
	EventKey string `json:"eventKey"`
	// RepositoryUUID is the provider repository UUID.
	RepositoryUUID string `json:"repositoryUuid"`
	// RepositoryFullName is the workspace and repository slug.
	RepositoryFullName string `json:"repositoryFullName"`
	// WorkspaceSlug identifies the workspace, when supplied by Bitbucket.
	WorkspaceSlug string `json:"workspaceSlug,omitempty"`
	// ProjectKey identifies a project event, when supplied by Bitbucket.
	ProjectKey string `json:"projectKey,omitempty"`
	// IssueID identifies an issue event, when supplied by Bitbucket.
	IssueID int64 `json:"issueId,omitempty"`
	// PullRequestID is the repository-local ID for pull request events.
	PullRequestID int64 `json:"pullRequestId,omitempty"`
	// CommentID identifies a pull request comment for comment events.
	CommentID int64 `json:"commentId,omitempty"`
	// PushChanges contains branch changes for repo:push events.
	PushChanges []RepositoryPushChange `json:"pushChanges,omitempty"`
}

type repositoryWebhookMessage struct {
	Workspace struct {
		Slug string `json:"slug"`
	} `json:"workspace"`
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
	Issue struct {
		ID int64 `json:"id"`
	} `json:"issue"`
	Repository struct {
		UUID     string `json:"uuid"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	PullRequest struct {
		ID int64 `json:"id"`
	} `json:"pullrequest"`
	Comment struct {
		ID int64 `json:"id"`
	} `json:"comment"`
	Push struct {
		Changes []struct {
			Old *struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Target struct {
					Hash string `json:"hash"`
				} `json:"target"`
			} `json:"old"`
			New *struct {
				Type   string `json:"type"`
				Name   string `json:"name"`
				Target struct {
					Hash string `json:"hash"`
				} `json:"target"`
			} `json:"new"`
		} `json:"changes"`
	} `json:"push"`
}

// Validate checks that each selected event key is supported and distinct.
func (configuration RepositoryEventReceivedTriggerConfiguration) Validate() error {
	seen := map[string]bool{}
	for _, eventKey := range configuration.EventKeys {
		if !slices.Contains(repositoryEventKeys, eventKey) || seen[eventKey] {
			return fmt.Errorf("repositoryEventReceived eventKeys must be distinct supported values")
		}
		seen[eventKey] = true
	}
	return nil
}

func (configuration RepositoryEventReceivedTriggerConfiguration) accepts(event sdkgo.TriggerEvent[RepositoryEvent]) bool {
	return len(configuration.EventKeys) == 0 || slices.Contains(configuration.EventKeys, event.Payload.EventKey)
}

// RepositoryEventReceivedWebhookHandler returns the HMAC-verifying endpoint for this connection.
// Mount it at the public HTTPS URL configured under Bitbucket repository Webhooks.
func (connection Connection) RepositoryEventReceivedWebhookHandler() (http.Handler, error) {
	if err := connection.validate(); err != nil {
		return nil, err
	}
	return connection.client.repositoryEventReceivedWebhookEndpoint(connection.reference)
}

func (client *Client) repositoryEventReceivedTriggerSource(connection sdkgo.ConnectionRef, configuration RepositoryEventReceivedTriggerConfiguration) sdkgo.TriggerSource[RepositoryEvent] {
	if err := configuration.Validate(); err != nil {
		panic(err)
	}
	endpoint, err := client.repositoryEventReceivedWebhookEndpoint(connection)
	if err != nil {
		panic(err)
	}
	return endpoint.NewSource(configuration.accepts)
}

func (client *Client) repositoryEventReceivedWebhookEndpoint(connection sdkgo.ConnectionRef) (*webhooktrigger.Endpoint[Credentials, RepositoryEvent], error) {
	client.repositoryEventEndpointsMu.Lock()
	defer client.repositoryEventEndpointsMu.Unlock()
	if endpoint, found := client.repositoryEventEndpoints[connection]; found {
		return endpoint, nil
	}
	endpoint, err := webhooktrigger.NewEndpoint(webhooktrigger.EndpointConfig[Credentials, RepositoryEvent]{
		ConnectorID: ConnectorID, TriggerName: RepositoryEventReceivedTriggerDefinition.Trigger.TriggerName,
		Connection: connection, Credentials: client.credentials, MaxBodyBytes: client.maxResponseBytes,
		VerifyRequest: func(request webhooktrigger.Request, _ Credentials) error {
			return verifyRepositoryEventRequest(request, client.webhookSecret)
		}, DecodeEvent: decodeRepositoryEventRequest,
	})
	if err != nil {
		return nil, fmt.Errorf("Bitbucket repository event endpoint: %w", err)
	}
	client.repositoryEventEndpoints[connection] = endpoint
	return endpoint, nil
}

func verifyRepositoryEventRequest(request webhooktrigger.Request, webhookSecret sdkgo.SecretString) error {
	secret := webhookSecret.Reveal()
	if secret == "" {
		return webhooktrigger.ErrVerificationUnavailable
	}
	signature := strings.TrimSpace(request.Header.Get("X-Hub-Signature"))
	encoded, found := strings.CutPrefix(signature, "sha256=")
	if !found {
		return errRepositoryEventSignatureInvalid
	}
	provided, err := hex.DecodeString(encoded)
	if err != nil || len(provided) != sha256.Size {
		return errRepositoryEventSignatureInvalid
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(request.Body)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return errRepositoryEventSignatureInvalid
	}
	return nil
}

func decodeRepositoryEventRequest(request webhooktrigger.Request) (sdkgo.TriggerEvent[RepositoryEvent], bool, error) {
	eventKey := strings.TrimSpace(request.Header.Get("X-Event-Key"))
	if !slices.Contains(repositoryEventKeys, eventKey) {
		return sdkgo.TriggerEvent[RepositoryEvent]{}, false, nil
	}
	requestUUID := strings.TrimSpace(request.Header.Get("X-Request-UUID"))
	if len(requestUUID) < 8 || len(requestUUID) > 128 || strings.ContainsAny(requestUUID, " \t\r\n/") {
		return sdkgo.TriggerEvent[RepositoryEvent]{}, false, errRepositoryEventMalformed
	}
	var message repositoryWebhookMessage
	if err := json.Unmarshal(request.Body, &message); err != nil {
		return sdkgo.TriggerEvent[RepositoryEvent]{}, false, errRepositoryEventMalformed
	}
	if eventKey != "project:updated" && (message.Repository.UUID == "" || message.Repository.FullName == "") {
		return sdkgo.TriggerEvent[RepositoryEvent]{}, false, errRepositoryEventMalformed
	}
	event := RepositoryEvent{EventKey: eventKey, RepositoryUUID: message.Repository.UUID, RepositoryFullName: message.Repository.FullName,
		WorkspaceSlug: message.Workspace.Slug, ProjectKey: message.Project.Key, IssueID: message.Issue.ID}
	if eventKey == RepositoryPushEventKey {
		if len(message.Push.Changes) == 0 {
			return sdkgo.TriggerEvent[RepositoryEvent]{}, false, errRepositoryEventMalformed
		}
		for _, change := range message.Push.Changes {
			item := RepositoryPushChange{}
			if change.Old != nil {
				item.OldRefType, item.OldTargetHash = change.Old.Type, change.Old.Target.Hash
				if change.Old.Type == "branch" {
					item.OldBranch = change.Old.Name
				}
				if change.Old.Type == "tag" {
					item.OldTag = change.Old.Name
				}
			}
			if change.New != nil {
				item.NewRefType, item.NewTargetHash = change.New.Type, change.New.Target.Hash
				if change.New.Type == "branch" {
					item.NewBranch = change.New.Name
				}
				if change.New.Type == "tag" {
					item.NewTag = change.New.Name
				}
			}
			if item.OldBranch != "" || item.NewBranch != "" || item.OldTag != "" || item.NewTag != "" {
				event.PushChanges = append(event.PushChanges, item)
			}
		}
		if len(event.PushChanges) == 0 {
			return sdkgo.TriggerEvent[RepositoryEvent]{}, false, nil
		}
	} else if strings.HasPrefix(eventKey, "pullrequest:") {
		if message.PullRequest.ID <= 0 {
			return sdkgo.TriggerEvent[RepositoryEvent]{}, false, errRepositoryEventMalformed
		}
		event.PullRequestID = message.PullRequest.ID
		if strings.HasPrefix(eventKey, "pullrequest:comment_") {
			if message.Comment.ID <= 0 {
				return sdkgo.TriggerEvent[RepositoryEvent]{}, false, errRepositoryEventMalformed
			}
			event.CommentID = message.Comment.ID
		}
	}
	return sdkgo.TriggerEvent[RepositoryEvent]{ID: requestUUID + ":" + eventKey, OccurredAt: request.ReceivedAt.UTC(), Payload: event}, true, nil
}
