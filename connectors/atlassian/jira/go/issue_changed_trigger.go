// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// createdUpdatedTolerance is how close Jira's created and updated times must be for a change to count as
// a creation. Search reports no change type, so the kind is derived from these timestamps.
const createdUpdatedTolerance = time.Second

// IssueChangeKind is the derived kind of one issueChanged delivery.
type IssueChangeKind string

const (
	// IssueChangeCreated is a new issue: Jira's updated time is within one second of its created time.
	IssueChangeCreated IssueChangeKind = "created"
	// IssueChangeUpdated is any other change that moved the issue's updated time, such as an edited field,
	// a transition, an assignment, or a comment, including an edit made soon after creation but before the
	// next poll read the issue.
	IssueChangeUpdated IssueChangeKind = "updated"
)

// IssueChangedTriggerConfiguration is the issueChanged binding configuration that the project picker saves
// in Dex Web. Changing the project of a binding that already polled makes its next poll record a new
// baseline for the new project.
type IssueChangedTriggerConfiguration struct {
	// ProjectID is the numeric Jira project ID from the project picker, such as 10000. It is preferred to
	// ProjectKey because it survives a project key change.
	ProjectID string `json:"projectId,omitempty"`
	// ProjectKey is the project key, such as OPS. It selects the project when ProjectID is blank, as
	// when the key was typed instead of picked. Blank ProjectID and ProjectKey watch every project the
	// connection's account can browse.
	ProjectKey string `json:"projectKey,omitempty"`
	// ProjectName is the picked project's display name, kept for display only.
	ProjectName string `json:"projectName,omitempty"`
}

// Validate rejects a project ID that is not numeric and a project key that is not an uppercase Jira key;
// blank values are valid.
func (configuration IssueChangedTriggerConfiguration) Validate() error {
	if configuration.ProjectID != "" && !numericIDPattern.MatchString(configuration.ProjectID) {
		return errors.New("issueChanged projectId must be a numeric Jira project ID such as 10000")
	}
	if configuration.ProjectKey != "" && !projectKeyPattern.MatchString(configuration.ProjectKey) {
		return errors.New("issueChanged projectKey must be an uppercase Jira project key such as OPS")
	}
	return nil
}

// watchedProject returns the project the binding watches as one bounded JQL clause value, and the stable
// identity stored in the cursor; both are empty when every browsable project is watched.
func (configuration IssueChangedTriggerConfiguration) watchedProject() (clauseValue string, identity string) {
	switch {
	case configuration.ProjectID != "":
		return configuration.ProjectID, "id:" + configuration.ProjectID
	case configuration.ProjectKey != "":
		return MustQuoteJQLString(configuration.ProjectKey), "key:" + configuration.ProjectKey
	default:
		return "", ""
	}
}

// IssueChangedEvent is one issue created or updated in the watched scope, as Jira's enhanced search
// returned it. Several edits made between two polls arrive as one change carrying the latest state.
type IssueChangedEvent struct {
	// CloudID is the Jira site the issue belongs to.
	CloudID string `json:"cloudId"`
	// ChangeKind is the derived change kind; see IssueChangeKind.
	ChangeKind IssueChangeKind `json:"changeKind"`
	// Issue is the issue's standard fields from the search page; Description is never set.
	Issue Issue `json:"issue"`
}

// newIssueChangedTriggerEvent converts one search result. Its ID is a pure function of the issue version,
// so a refetch after a crash yields the same ID.
func newIssueChangedTriggerEvent(cloudID string, issue Issue) sdkgo.TriggerEvent[IssueChangedEvent] {
	return sdkgo.TriggerEvent[IssueChangedEvent]{
		ID:         issueChangeID(cloudID, issue),
		OccurredAt: issue.UpdatedAt.UTC(),
		Payload:    IssueChangedEvent{CloudID: cloudID, ChangeKind: issueChangeKind(issue), Issue: issue},
	}
}

// issueChangeID hashes the site, the issue ID, and the updated instant, which identify one issue version.
func issueChangeID(cloudID string, issue Issue) string {
	version := strings.Join([]string{cloudID, issue.ID, strconv.FormatInt(issue.UpdatedAt.UnixMilli(), 10)}, "\x00")
	digest := sha256.Sum256([]byte(version))
	return "jira-" + hex.EncodeToString(digest[:16])
}

func issueChangeKind(issue Issue) IssueChangeKind {
	if !issue.CreatedAt.IsZero() && issue.UpdatedAt.Sub(issue.CreatedAt).Abs() <= createdUpdatedTolerance {
		return IssueChangeCreated
	}
	return IssueChangeUpdated
}

func issueChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("jira issueChanged configuration: %w", err)
}
