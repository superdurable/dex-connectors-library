// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package employeechangesweep demonstrates BambooHR's employee change history as the poll source
// for onboarding and offboarding in one Flow started from Dex Web Start Flow: list the employees
// inserted, updated, or deleted since a cursor, page by page, and complete with the cursor the
// next sweep starts from.
package employeechangesweep

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "BambooHREmployeeChangeSweep"
	// ConnectionName is the static Dex Web connection for BambooHR, shared with the onboarding example.
	ConnectionName = "bamboohr-company"

	recordSweepWindowStepType      = "RecordSweepWindow"
	listEmployeeChangesStepType    = "ListEmployeeChanges"
	collectEmployeeChangesStepType = "CollectEmployeeChanges"

	// DefaultPageSize is the changes read per page when Start Flow leaves pageSize blank.
	DefaultPageSize = 100
	// DefaultMaxPages is the pages one sweep reads when Start Flow leaves maxPages blank.
	DefaultMaxPages = 5
	// MaxPages bounds the pages, and so the changes, one sweep keeps in its Attribute.
	MaxPages = 10
)

var sweepAttribute = dex.DefineAttribute[EmployeeChangeSweep]("bamboohr-employee-change-sweep")

// Input is the sweep window entered in Dex Web Start Flow.
type Input struct {
	// Since is the RFC 3339 instant to sweep from, such as 2026-09-24T00:00:00Z; pass the previous
	// sweep's nextCursor.since to continue.
	Since string `json:"since"`
	// AfterEmployeeID is the previous sweep's nextCursor.afterEmployeeId, or empty.
	AfterEmployeeID string `json:"afterEmployeeId,omitempty"`
	// ChangeType is inserted for onboarding, updated, deleted, or empty for every change.
	ChangeType bamboohr.EmployeeChangeType `json:"changeType,omitempty"`
	// PageSize is 1 to 1000 changes per page; zero uses DefaultPageSize.
	PageSize int `json:"pageSize,omitempty"`
	// MaxPages is 1 to 10 pages per sweep; zero uses DefaultMaxPages.
	MaxPages int `json:"maxPages,omitempty"`
}

// SweepPage is one page request: where it starts and what it keeps.
type SweepPage struct {
	// Cursor is where the page starts.
	Cursor bamboohr.EmployeeChangeCursor `json:"cursor"`
	// ChangeType keeps one change type, or every change when empty.
	ChangeType bamboohr.EmployeeChangeType `json:"changeType,omitempty"`
	// PageSize is the most changes the page returns.
	PageSize int `json:"pageSize"`
}

// EmployeeChangeSweep is the Flow result and the value of its Attribute.
type EmployeeChangeSweep struct {
	// ChangeType is the change type swept, or empty for every change.
	ChangeType bamboohr.EmployeeChangeType `json:"changeType,omitempty"`
	// PageSize is the most changes one page returns.
	PageSize int `json:"pageSize"`
	// MaxPages is the most pages this sweep reads.
	MaxPages int `json:"maxPages"`
	// PagesRead counts the pages read so far.
	PagesRead int `json:"pagesRead"`
	// Changes lists every change read, oldest first.
	Changes []bamboohr.EmployeeChange `json:"changes"`
	// NextCursor is where the next sweep starts.
	NextCursor bamboohr.EmployeeChangeCursor `json:"nextCursor"`
	// IsCaughtUp reports that no change after NextCursor remained when the sweep ended; false means
	// MaxPages stopped it, so start the next sweep at once.
	IsCaughtUp bool `json:"isCaughtUp"`
}

// Flow sweeps BambooHR's employee change history from one cursor.
type Flow struct {
	dex.FlowDefaults
	connection bamboohr.Connection
}

// NewFlow binds the BambooHR Connection at registration time.
func NewFlow(connection bamboohr.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and BambooHR connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSweepWindow{}),
		dex.DefineStep(bamboohr.NewListEmployeeChangesStep(bamboohr.ListEmployeeChangesStepConfig[SweepPage]{
			StepType: listEmployeeChangesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "bamboohr", GroupLabel: "BambooHR",
				Explanation: "List the oldest employee changes after the cursor, one page at a time.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListEmployeeChangesInput,
			Listed: sdkgo.GoTo(collectEmployeeChanges{}),
		})),
		dex.DefineStep(collectEmployeeChanges{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the sweep Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sweepAttribute}}
}

// GetDexSummary returns the sweep so far.
//
// dex:field attribute-key:bamboohr-employee-change-sweep value-type:json editable:false description:"Employee change sweep"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sweep, err := optionalSweep(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bamboohr-employee-change-sweep": sweep}}, nil
}

// GetDexDisplay returns the changes read and the next cursor.
//
// dex:field attribute-key:bamboohr-employee-change-sweep value-type:json editable:false description:"Changes read, pages, next cursor, and whether the sweep caught up"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	sweep, err := optionalSweep(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bamboohr-employee-change-sweep": sweep}}, nil
}

// MapToListEmployeeChangesInput lists one page after the cursor.
func MapToListEmployeeChangesInput(page SweepPage) bamboohr.ListEmployeeChangesInput {
	return bamboohr.ListEmployeeChangesInput{
		Since: page.Cursor.Since, AfterEmployeeID: page.Cursor.AfterEmployeeID, ChangeType: page.ChangeType, Limit: page.PageSize,
	}
}

// BuildFirstSweepPage validates Start Flow input and returns the sweep and its first page.
func BuildFirstSweepPage(input Input) (EmployeeChangeSweep, SweepPage, error) {
	since, err := time.Parse(time.RFC3339, strings.TrimSpace(input.Since))
	if err != nil {
		return EmployeeChangeSweep{}, SweepPage{}, fmt.Errorf("since %q must be an RFC 3339 instant such as 2026-09-24T00:00:00Z", input.Since)
	}
	switch input.ChangeType {
	case "", bamboohr.EmployeeChangeTypeInserted, bamboohr.EmployeeChangeTypeUpdated, bamboohr.EmployeeChangeTypeDeleted:
	default:
		return EmployeeChangeSweep{}, SweepPage{}, errors.New("changeType must be inserted, updated, deleted, or empty")
	}
	sweep := EmployeeChangeSweep{ChangeType: input.ChangeType, PageSize: input.PageSize, MaxPages: input.MaxPages, Changes: []bamboohr.EmployeeChange{}}
	if sweep.PageSize == 0 {
		sweep.PageSize = DefaultPageSize
	}
	if sweep.MaxPages == 0 {
		sweep.MaxPages = DefaultMaxPages
	}
	if sweep.PageSize < 1 || sweep.PageSize > bamboohr.MaxEmployeeChangeLimit || sweep.MaxPages < 1 || sweep.MaxPages > MaxPages {
		return EmployeeChangeSweep{}, SweepPage{}, fmt.Errorf("pageSize must be 1 to %d and maxPages 1 to %d", bamboohr.MaxEmployeeChangeLimit, MaxPages)
	}
	sweep.NextCursor = bamboohr.EmployeeChangeCursor{Since: since.UTC(), AfterEmployeeID: strings.TrimSpace(input.AfterEmployeeID)}
	return sweep, SweepPage{Cursor: sweep.NextCursor, ChangeType: sweep.ChangeType, PageSize: sweep.PageSize}, nil
}

func optionalSweep(ctx dex.Context) (EmployeeChangeSweep, error) {
	sweep, err := sweepAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return EmployeeChangeSweep{}, nil
	}
	return sweep, err
}

// dex:group group-id:sweep group-label:"Change sweep"
// dex:explanation text:"Validate the sweep window and record it before calling BambooHR."
type recordSweepWindow struct {
	dex.StepDefaults
}

func (recordSweepWindow) GetStepType() string { return recordSweepWindowStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordSweepWindow) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordSweepWindow) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	sweep, page, err := BuildFirstSweepPage(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := sweepAttribute.Set(ctx, sweep); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SweepPage](listEmployeeChangesStepType), page), nil
}

// dex:group group-id:sweep group-label:"Change sweep"
// dex:explanation text:"Keep the page's changes, read the next page while more remain and pages are left, then complete with the next cursor."
type collectEmployeeChanges struct {
	dex.StepDefaultsNoWaitFor[bamboohr.ListEmployeeChangesResult]
}

func (collectEmployeeChanges) GetStepType() string { return collectEmployeeChangesStepType }

func (collectEmployeeChanges) Execute(ctx dex.Context, result bamboohr.ListEmployeeChangesResult) (*dex.StepDecision, error) {
	sweep, err := sweepAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	sweep.PagesRead++
	sweep.Changes = append(sweep.Changes, result.Value.Changes...)
	sweep.NextCursor = result.Value.NextCursor
	sweep.IsCaughtUp = !result.Value.HasMore
	if err := sweepAttribute.Set(ctx, sweep); err != nil {
		return nil, err
	}
	if result.Value.HasMore && sweep.PagesRead < sweep.MaxPages {
		return dex.GoTo(sdkgo.StepRef[SweepPage](listEmployeeChangesStepType),
			SweepPage{Cursor: sweep.NextCursor, ChangeType: sweep.ChangeType, PageSize: sweep.PageSize}), nil
	}
	return dex.GracefulComplete(sweep), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
