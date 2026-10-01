// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listEmployeeChangesOperation = "listEmployeeChanges"

	// DefaultEmployeeChangeLimit is the number of changes listEmployeeChanges returns when Limit is zero.
	DefaultEmployeeChangeLimit = 100
	// MaxEmployeeChangeLimit is the largest Limit listEmployeeChanges accepts.
	MaxEmployeeChangeLimit = 1000

	// changeQueryOverlap widens since, so a change at the cursor instant is returned whether BambooHR's since is inclusive or not.
	changeQueryOverlap = time.Second
)

var changeActionPattern = regexp.MustCompile(`^[A-Za-z]{1,32}$`)

// EmployeeChangeType filters listEmployeeChanges to one BambooHR change type.
type EmployeeChangeType string

const (
	// EmployeeChangeTypeInserted lists employees added since the cursor, the onboarding poll.
	EmployeeChangeTypeInserted EmployeeChangeType = "inserted"
	// EmployeeChangeTypeUpdated lists employees whose record, employment status, job information,
	// or compensation changed since the cursor; a termination is an update.
	EmployeeChangeTypeUpdated EmployeeChangeType = "updated"
	// EmployeeChangeTypeDeleted lists employee records deleted since the cursor.
	EmployeeChangeTypeDeleted EmployeeChangeType = "deleted"
)

// EmployeeChangeAction is BambooHR's change action, passed through as BambooHR writes it.
type EmployeeChangeAction string

const (
	// EmployeeChangeActionInserted is BambooHR's Inserted action.
	EmployeeChangeActionInserted EmployeeChangeAction = "Inserted"
	// EmployeeChangeActionUpdated is BambooHR's Updated action.
	EmployeeChangeActionUpdated EmployeeChangeAction = "Updated"
	// EmployeeChangeActionDeleted is BambooHR's Deleted action.
	EmployeeChangeActionDeleted EmployeeChangeAction = "Deleted"
)

// EmployeeChangeCursor is a position in BambooHR's change history: changes after LastChanged,
// or at LastChanged for an employee ID above AfterEmployeeID.
type EmployeeChangeCursor struct {
	// Since is the change instant the cursor stands at, in UTC.
	Since time.Time `json:"since"`
	// AfterEmployeeID is the last employee ID already returned at Since; empty includes every
	// change at Since.
	AfterEmployeeID string `json:"afterEmployeeId,omitempty"`
}

// ListEmployeeChangesInput is one poll of BambooHR's employee change history.
type ListEmployeeChangesInput struct {
	// Since lists changes at or after this instant. It is required; store the previous call's
	// NextCursor and pass it back to continue.
	Since time.Time `json:"since"`
	// AfterEmployeeID skips changes at exactly Since for this employee ID and every lower ID, as
	// NextCursor sets it. Empty includes every change at Since.
	AfterEmployeeID string `json:"afterEmployeeId,omitempty"`
	// ChangeType keeps one change type; blank lists inserted, updated, and deleted employees.
	ChangeType EmployeeChangeType `json:"changeType,omitempty"`
	// Limit is the most changes to return, 1 to 1000; zero uses DefaultEmployeeChangeLimit.
	Limit int `json:"limit,omitempty"`
}

// EmployeeChange is one changed employee. BambooHR reports only each employee's latest change.
type EmployeeChange struct {
	// EmployeeID is BambooHR's internal employee ID.
	EmployeeID string `json:"employeeId"`
	// Action is BambooHR's change action, such as Inserted.
	Action EmployeeChangeAction `json:"action"`
	// LastChanged is when the employee last changed, in UTC.
	LastChanged time.Time `json:"lastChanged"`
}

// ListEmployeeChangesOutput is one page of changes, oldest first.
type ListEmployeeChangesOutput struct {
	// Changes lists the changes after the cursor, ordered by LastChanged and then employee ID.
	Changes []EmployeeChange `json:"changes"`
	// NextCursor is where the next poll starts: after the last returned change, or the input
	// cursor when nothing changed.
	NextCursor EmployeeChangeCursor `json:"nextCursor"`
	// HasMore reports that more changes than Limit exist after the cursor, so poll again now.
	HasMore bool `json:"hasMore,omitempty"`
}

// ListEmployeeChangesOperation is the listEmployeeChanges Query.
type ListEmployeeChangesOperation struct {
	client *Client
}

type changedEmployeesWire struct {
	Employees json.RawMessage `json:"employees"`
}

type changedEmployeeWire struct {
	ID          json.RawMessage `json:"id"`
	Action      string          `json:"action"`
	LastChanged string          `json:"lastChanged"`
}

// Definition returns the immutable connector operation definition.
func (ListEmployeeChangesOperation) Definition() sdkgo.QueryDefinition {
	return ListEmployeeChangesDefinition
}

// Invoke reads GET /api/v1/employees/changed?since=...&type=... one second before the cursor,
// keeps the changes after it, and returns the oldest Limit of them.
func (operation ListEmployeeChangesOperation) Invoke(call sdkgo.Call, input ListEmployeeChangesInput) sdkgo.QueryAttempt[ListEmployeeChangesOutput] {
	if input.Limit == 0 {
		input.Limit = DefaultEmployeeChangeLimit
	}
	if err := validateListEmployeeChangesInput(input); err != nil {
		return sdkgo.NewQueryBranch(ListEmployeeChangesBranchDefect, ListEmployeeChangesOutput{}, bambooHRFailurePointer(sdkgo.FailureValidation, listEmployeeChangesOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, listEmployeeChangesOperation)
	if failure != nil {
		return sdkgo.NewQueryBranch(ListEmployeeChangesBranchDefect, ListEmployeeChangesOutput{}, failure, sdkgo.Receipt{})
	}
	cursor := EmployeeChangeCursor{Since: input.Since.UTC(), AfterEmployeeID: input.AfterEmployeeID}
	query := url.Values{"since": {cursor.Since.Add(-changeQueryOverlap).Truncate(time.Second).Format(time.RFC3339)}}
	if input.ChangeType != "" {
		query.Set("type", string(input.ChangeType))
	}
	result := operation.client.exchange(call, credentials, listEmployeeChangesOperation, bambooHRRequest{method: http.MethodGet, path: "/employees/changed", query: query})
	receipt := operation.client.receipt(call, "")
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRateLimited, exchangeLimitExceeded, exchangeNotSent, exchangeUnavailable:
		return sdkgo.NewQueryRetry[ListEmployeeChangesOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewQueryBranch(ListEmployeeChangesBranchInvalidResponse, ListEmployeeChangesOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewQueryBranch(ListEmployeeChangesBranchDefect, ListEmployeeChangesOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListEmployeeChangesBranchProviderRejected, ListEmployeeChangesOutput{}, &result.failure, receipt)
	}
	changes, err := decodeEmployeeChanges(result.response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(ListEmployeeChangesBranchInvalidResponse, ListEmployeeChangesOutput{},
			bambooHRFailurePointer(sdkgo.FailureProtocol, listEmployeeChangesOperation, "BambooHR returned an invalid change list: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListEmployeeChangesBranchListed, pageEmployeeChanges(changes, cursor, input.Limit), nil, receipt)
}

func validateListEmployeeChangesInput(input ListEmployeeChangesInput) error {
	if input.Since.IsZero() {
		return errors.New("since is required; pass the previous call's nextCursor to continue")
	}
	if input.AfterEmployeeID != "" {
		if err := validateEmployeeID("afterEmployeeId", input.AfterEmployeeID); err != nil {
			return err
		}
	}
	switch input.ChangeType {
	case "", EmployeeChangeTypeInserted, EmployeeChangeTypeUpdated, EmployeeChangeTypeDeleted:
	default:
		return errors.New("changeType must be inserted, updated, deleted, or blank")
	}
	if input.Limit < 1 || input.Limit > MaxEmployeeChangeLimit {
		return fmt.Errorf("limit must be 1 to %d", MaxEmployeeChangeLimit)
	}
	return nil
}

func decodeEmployeeChanges(body []byte) ([]EmployeeChange, error) {
	var document changedEmployeesWire
	if err := json.Unmarshal(body, &document); err != nil || len(document.Employees) == 0 {
		return nil, errors.New("the response is not a change list")
	}
	// A PHP encoder writes an empty map as [], which is how a period without changes may arrive.
	if string(bytes.TrimSpace(document.Employees)) == "[]" {
		return nil, nil
	}
	var entries map[string]changedEmployeeWire
	if err := json.Unmarshal(document.Employees, &entries); err != nil || entries == nil {
		return nil, errors.New("employees is not a map of changes")
	}
	changes := make([]EmployeeChange, 0, len(entries))
	for key, entry := range entries {
		employeeID := key
		if len(entry.ID) != 0 {
			decodedID, err := decodeEmployeeIDValue(entry.ID)
			if err != nil || decodedID != key {
				return nil, errors.New("a change names an invalid employee ID")
			}
		}
		if validateEmployeeID("id", employeeID) != nil {
			return nil, errors.New("a change names an invalid employee ID")
		}
		if !changeActionPattern.MatchString(entry.Action) {
			return nil, fmt.Errorf("employee %s has an invalid action", employeeID)
		}
		lastChanged, err := time.Parse(time.RFC3339, entry.LastChanged)
		if err != nil {
			return nil, fmt.Errorf("employee %s has an invalid lastChanged", employeeID)
		}
		changes = append(changes, EmployeeChange{EmployeeID: employeeID, Action: EmployeeChangeAction(entry.Action), LastChanged: lastChanged.UTC()})
	}
	return changes, nil
}

// pageEmployeeChanges keeps the changes after the cursor and returns the oldest limit of them.
func pageEmployeeChanges(changes []EmployeeChange, cursor EmployeeChangeCursor, limit int) ListEmployeeChangesOutput {
	after := make([]EmployeeChange, 0, len(changes))
	for _, change := range changes {
		if isChangeAfterCursor(change, cursor) {
			after = append(after, change)
		}
	}
	slices.SortFunc(after, func(left EmployeeChange, right EmployeeChange) int {
		if comparison := left.LastChanged.Compare(right.LastChanged); comparison != 0 {
			return comparison
		}
		return compareEmployeeIDs(left.EmployeeID, right.EmployeeID)
	})
	output := ListEmployeeChangesOutput{Changes: after, NextCursor: cursor}
	if len(after) > limit {
		output.Changes, output.HasMore = after[:limit], true
	}
	if len(output.Changes) > 0 {
		last := output.Changes[len(output.Changes)-1]
		output.NextCursor = EmployeeChangeCursor{Since: last.LastChanged, AfterEmployeeID: last.EmployeeID}
	}
	return output
}

func isChangeAfterCursor(change EmployeeChange, cursor EmployeeChangeCursor) bool {
	switch comparison := change.LastChanged.Compare(cursor.Since); {
	case comparison > 0:
		return true
	case comparison < 0:
		return false
	default:
		return cursor.AfterEmployeeID == "" || compareEmployeeIDs(change.EmployeeID, cursor.AfterEmployeeID) > 0
	}
}
