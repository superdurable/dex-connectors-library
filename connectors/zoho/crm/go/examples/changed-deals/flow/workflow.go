// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package changeddeals demonstrates Zoho CRM's listModifiedRecords in one Flow started from Dex Web
// Start Flow: read the deals changed since an instant or a stored cursor, page by page, and complete
// with the changed deals and the cursor the next poll continues from.
package changeddeals

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "ZohoCRMChangedDeals"
	// ConnectionName is the static Dex Web connection for Zoho CRM.
	ConnectionName = "zoho-crm-sales"

	recordChangedDealsRequestStepType = "RecordChangedDealsRequest"
	listChangedZohoDealsStepType      = "ListChangedZohoDeals"
	collectChangedZohoDealsStepType   = "CollectChangedZohoDeals"

	// PageLimit is the number of deals one page reads.
	PageLimit = 50
	// DefaultMaxPages is the page budget when Input.MaxPages is zero.
	DefaultMaxPages = 5
	maximumMaxPages = 20
)

var changedDealsAttribute = dex.DefineAttribute[ChangedDeals]("zoho-crm-changed-deals")

// Input starts the feed at an instant or continues it from a cursor an earlier run returned.
type Input struct {
	// ModifiedSince is an RFC 3339 instant such as 2026-01-28T13:00:00Z; leave it blank when Cursor is set.
	ModifiedSince string `json:"modifiedSince,omitempty"`
	// Cursor is the cursor an earlier run completed with; leave it blank when ModifiedSince is set.
	Cursor string `json:"cursor,omitempty"`
	// MaxPages bounds the pages one run reads, 1 to 20; zero uses DefaultMaxPages.
	MaxPages int `json:"maxPages,omitempty"`
}

// ChangedDealsPage is where the next page starts.
type ChangedDealsPage struct {
	// ModifiedSince starts the first page of a new feed.
	ModifiedSince time.Time `json:"modifiedSince,omitzero"`
	// Cursor continues a feed.
	Cursor string `json:"cursor,omitempty"`
}

// ChangedDeal is one changed deal as listModifiedRecords returned it.
type ChangedDeal struct {
	// ID is the deal's record ID.
	ID string `json:"id"`
	// DealName is the deal's name.
	DealName string `json:"dealName,omitempty"`
	// Stage is the deal's Zoho CRM stage.
	Stage string `json:"stage,omitempty"`
	// ModifiedAt is the deal's Modified_Time.
	ModifiedAt time.Time `json:"modifiedAt"`
}

// ChangedDeals is the feed's progress, the Flow's single Attribute and its result.
type ChangedDeals struct {
	// MaxPages is the page budget of this run.
	MaxPages int `json:"maxPages"`
	// Pages counts the pages read.
	Pages int `json:"pages"`
	// Deals are the changed deals, oldest change first.
	Deals []ChangedDeal `json:"deals,omitempty"`
	// Cursor is where the next page or the next poll continues.
	Cursor string `json:"cursor,omitempty"`
	// HasMore reports that the page budget ended before the feed did.
	HasMore bool `json:"hasMore,omitempty"`
}

// Flow reads one run of the changed-deals feed.
type Flow struct {
	dex.FlowDefaults
	connection crm.Connection
}

// NewFlow binds the Zoho CRM Connection at registration time.
func NewFlow(connection crm.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Zoho CRM connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordChangedDealsRequest{}),
		dex.DefineStep(crm.NewListModifiedRecordsStep(crm.ListModifiedRecordsStepConfig[ChangedDealsPage]{
			StepType: listChangedZohoDealsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-crm", GroupLabel: "Zoho CRM",
				Explanation: "List one page of deals changed after the cursor, oldest change first.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListModifiedRecordsInput,
			Listed: sdkgo.GoTo(collectChangedZohoDeals{}),
		})),
		dex.DefineStep(collectChangedZohoDeals{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the changed-deals Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{changedDealsAttribute}}
}

// GetDexSummary returns the feed's progress.
//
// dex:field attribute-key:zoho-crm-changed-deals value-type:json editable:false description:"Zoho CRM changed deals"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	changed, err := optionalChangedDeals(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zoho-crm-changed-deals": changed}}, nil
}

// GetDexDisplay returns the changed deals and the cursor.
//
// dex:field attribute-key:zoho-crm-changed-deals value-type:json editable:false description:"Changed deals, pages, and cursor"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	changed, err := optionalChangedDeals(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zoho-crm-changed-deals": changed}}, nil
}

// MapToListModifiedRecordsInput reads one page of changed deals.
func MapToListModifiedRecordsInput(page ChangedDealsPage) crm.ListModifiedRecordsInput {
	return crm.ListModifiedRecordsInput{
		Module: crm.ModuleDeals, Fields: []string{crm.FieldDealName, crm.FieldStage},
		ModifiedSince: page.ModifiedSince, Cursor: page.Cursor, Limit: PageLimit,
	}
}

// BuildFirstPage validates Start Flow input and returns the first page and the page budget.
func BuildFirstPage(input Input) (ChangedDealsPage, int, error) {
	since, cursor := strings.TrimSpace(input.ModifiedSince), strings.TrimSpace(input.Cursor)
	maxPages := input.MaxPages
	if maxPages == 0 {
		maxPages = DefaultMaxPages
	}
	if maxPages < 1 || maxPages > maximumMaxPages {
		return ChangedDealsPage{}, 0, errors.New("maxPages must be 1 to 20")
	}
	switch {
	case (since == "") == (cursor == ""):
		return ChangedDealsPage{}, 0, errors.New("set modifiedSince to start the feed or cursor to continue it, not both")
	case cursor != "":
		if _, err := crm.BuildListModifiedRecordsQuery(MapToListModifiedRecordsInput(ChangedDealsPage{Cursor: cursor})); err != nil {
			return ChangedDealsPage{}, 0, errors.New("cursor must be a cursor an earlier run completed with")
		}
		return ChangedDealsPage{Cursor: cursor}, maxPages, nil
	}
	modifiedSince, err := time.Parse(time.RFC3339, since)
	if err != nil {
		return ChangedDealsPage{}, 0, errors.New("modifiedSince must be an RFC 3339 instant such as 2026-01-28T13:00:00Z")
	}
	return ChangedDealsPage{ModifiedSince: modifiedSince}, maxPages, nil
}

// ChangedDealsFromRecords reads the deal fields the page selected.
func ChangedDealsFromRecords(records []crm.Record) []ChangedDeal {
	deals := make([]ChangedDeal, 0, len(records))
	for _, record := range records {
		deal := ChangedDeal{ID: record.ID}
		deal.DealName, _ = record.StringField(crm.FieldDealName)
		deal.Stage, _ = record.StringField(crm.FieldStage)
		deal.ModifiedAt, _ = record.ModifiedAt()
		deals = append(deals, deal)
	}
	return deals
}

func optionalChangedDeals(ctx dex.Context) (ChangedDeals, error) {
	changed, err := changedDealsAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return ChangedDeals{}, nil
	}
	return changed, err
}

// dex:group group-id:feed group-label:"Changed deals"
// dex:explanation text:"Validate where the feed starts and record the page budget before calling Zoho CRM."
type recordChangedDealsRequest struct {
	dex.StepDefaults
}

func (recordChangedDealsRequest) GetStepType() string { return recordChangedDealsRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordChangedDealsRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordChangedDealsRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	page, maxPages, err := BuildFirstPage(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := changedDealsAttribute.Set(ctx, ChangedDeals{MaxPages: maxPages, Cursor: page.Cursor}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ChangedDealsPage](listChangedZohoDealsStepType), page), nil
}

// dex:group group-id:feed group-label:"Changed deals"
// dex:explanation text:"Keep the page's deals and its cursor; read the next page while more remain within the budget, otherwise complete."
type collectChangedZohoDeals struct {
	dex.StepDefaultsNoWaitFor[crm.ListModifiedRecordsResult]
}

func (collectChangedZohoDeals) GetStepType() string { return collectChangedZohoDealsStepType }

func (collectChangedZohoDeals) Execute(ctx dex.Context, result crm.ListModifiedRecordsResult) (*dex.StepDecision, error) {
	changed, err := changedDealsAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	changed.Pages++
	changed.Deals = append(changed.Deals, ChangedDealsFromRecords(result.Value.Records)...)
	changed.Cursor, changed.HasMore = result.Value.Cursor, result.Value.HasMore
	if err := changedDealsAttribute.Set(ctx, changed); err != nil {
		return nil, err
	}
	if changed.HasMore && changed.Pages < changed.MaxPages {
		return dex.GoTo(sdkgo.StepRef[ChangedDealsPage](listChangedZohoDealsStepType), ChangedDealsPage{Cursor: changed.Cursor}), nil
	}
	return dex.GracefulComplete(changed), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
