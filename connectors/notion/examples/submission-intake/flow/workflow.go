// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package submissionintake demonstrates every Notion operation in one Flow
// started from Dex Web Start Flow: it finds the submissions database by
// title, looks for a row with the submission's ID, updates that row or creates
// a new one, and reads the row back. Because Notion has no idempotency key, the
// Submission ID property is the business key: re-running the Flow with the same
// ID updates the existing row instead of adding a second one, and an uncertain
// create is reconciled by querying for that ID.
package submissionintake

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "NotionSubmissionIntake"
	// ConnectionName is the static Dex Web connection for Notion.
	ConnectionName = "notion-workspace"

	// SubmissionIDProperty is the text property that holds each row's submission ID.
	SubmissionIDProperty = "Submission ID"
	// EmailProperty is the email property that holds the submitter's address.
	EmailProperty = "Email"
	// SubmittedAtProperty is the date property that holds the submission time.
	SubmittedAtProperty = "Submitted at"

	recordSubmissionStepType           = "RecordNotionSubmission"
	findSubmissionsDataSourceStepType  = "FindNotionSubmissionsDataSource"
	resolveSubmissionsDataSourceType   = "ResolveNotionSubmissionsDataSource"
	findExistingSubmissionStepType     = "FindExistingNotionSubmission"
	routeSubmissionStepType            = "RouteNotionSubmission"
	createSubmissionRowStepType        = "CreateNotionSubmissionRow"
	recordCreatedSubmissionStepType    = "RecordCreatedNotionSubmission"
	recordUncertainSubmissionStepType  = "RecordUncertainNotionSubmission"
	reconcileSubmissionStepType        = "ReconcileNotionSubmission"
	routeReconciledSubmissionStepType  = "RouteReconciledNotionSubmission"
	updateSubmissionRowStepType        = "UpdateNotionSubmissionRow"
	recordUpdatedSubmissionStepType    = "RecordUpdatedNotionSubmission"
	readBackSubmissionStepType         = "ReadBackNotionSubmission"
	completeSubmissionStepType         = "CompleteNotionSubmission"
	recordRejectedCreateStepType       = "RecordRejectedNotionSubmissionCreate"
	recordRejectedUpdateStepType       = "RecordRejectedNotionSubmissionUpdate"
	maximumDatabaseTitleRunes          = 200
	maximumSubmissionIDRunes           = 100
	maximumNameRunes                   = 200
	maximumEmailBytes                  = 200
	maximumMessageRunes                = 10000
	searchPageSize                     = 20
	duplicateDetectionPageSize         = 2
	readBackMaximumTextCharacters      = 10000
	submissionRecordAttributeKey       = "notion-submission"
	submissionIDMismatchFailureMessage = "Notion row %s reads back Submission ID %q, not the submitted ID"
)

var submissionAttribute = dex.DefineAttribute[SubmissionRecord](submissionRecordAttributeKey)

// Phase is the durable progress of one submission.
type Phase string

const (
	// PhaseRecorded means the submission was validated and recorded.
	PhaseRecorded Phase = "recorded"
	// PhaseDataSourceResolved means exactly one data source has the database title.
	PhaseDataSourceResolved Phase = "dataSourceResolved"
	// PhaseCreateRequested means the new row was sent to Notion.
	PhaseCreateRequested Phase = "createRequested"
	// PhaseUpdateRequested means the existing row's update was sent to Notion.
	PhaseUpdateRequested Phase = "updateRequested"
	// PhaseCreated means a new row was created and read back.
	PhaseCreated Phase = "created"
	// PhaseUpdated means the existing row was updated and read back.
	PhaseUpdated Phase = "updated"
	// PhaseDataSourceNotFound means no shared data source has the database title.
	PhaseDataSourceNotFound Phase = "dataSourceNotFound"
	// PhaseDataSourceAmbiguous means several shared data sources have the database title.
	PhaseDataSourceAmbiguous Phase = "dataSourceAmbiguous"
	// PhaseDuplicateRows means several rows already hold the submission ID.
	PhaseDuplicateRows Phase = "duplicateRows"
	// PhaseCreateUncertain means the create's outcome is unknown and no row with the ID is visible yet.
	PhaseCreateUncertain Phase = "createUncertain"
	// PhaseRejected means Notion rejected the row, such as for a missing property.
	PhaseRejected Phase = "rejected"
)

// Input contains the submission fields entered in Dex Web Start Flow.
type Input struct {
	// DatabaseTitle is the exact title of the Notion database that receives submissions.
	DatabaseTitle string `json:"databaseTitle"`
	// SubmissionID is the form's unique submission ID, the row's business key.
	SubmissionID string `json:"submissionId"`
	// Name is the submitter's name, written to the title property.
	Name string `json:"name"`
	// Email is the submitter's plain email address.
	Email string `json:"email"`
	// Message is optional plain text written to a new row's page body.
	Message string `json:"message,omitempty"`
}

// SubmissionRecord is the Flow's durable state and completion output.
type SubmissionRecord struct {
	// Submission is the validated Start Flow input.
	Submission Input `json:"submission"`
	// SubmittedAt is the Flow start time written to the Submitted at property.
	SubmittedAt string `json:"submittedAt"`
	// Phase is the current or terminal progress.
	Phase Phase `json:"phase"`
	// DataSourceID is the data source that holds the submissions.
	DataSourceID string `json:"dataSourceId,omitempty"`
	// CandidateDataSourceIDs lists every data source with the title when the title is ambiguous.
	CandidateDataSourceIDs []string `json:"candidateDataSourceIds,omitempty"`
	// PageID is the row that holds the submission.
	PageID string `json:"pageId,omitempty"`
	// PageURL opens the row in Notion.
	PageURL string `json:"pageUrl,omitempty"`
	// IsExistingRow reports that the row existed and was updated rather than created.
	IsExistingRow bool `json:"existingRow"`
	// DuplicatePageIDs lists the rows that already hold the submission ID when there are several.
	DuplicatePageIDs []string `json:"duplicatePageIds,omitempty"`
	// StoredProperties is the plain text of every property read back from the row.
	StoredProperties map[string]string `json:"storedProperties,omitempty"`
	// StoredBodyText is the row's page body read back as plain text.
	StoredBodyText string `json:"storedBodyText,omitempty"`
	// UncertainCreate is the secret-safe reason the create's outcome was unknown.
	UncertainCreate *sdkgo.Failure `json:"uncertainCreate,omitempty"`
	// Rejection is Notion's secret-safe rejection of the row.
	Rejection *sdkgo.Failure `json:"rejection,omitempty"`
}

// Flow records one form submission as a Notion database row.
type Flow struct {
	dex.FlowDefaults
	connection notion.Connection
}

// NewFlow binds the Notion Connection.
func NewFlow(connection notion.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Notion Connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSubmission{}),
		dex.DefineStep(notion.NewSearchStep(notion.SearchStepConfig[SubmissionRecord]{
			StepType: findSubmissionsDataSourceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "notion", GroupLabel: "Notion",
				Explanation: "Search the data sources shared with the connection for the submissions database title.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindSubmissionsDataSourceInput,
			Searched: sdkgo.GoTo(resolveSubmissionsDataSource{}),
			NoMatch:  sdkgo.GoTo(resolveSubmissionsDataSource{}),
		})),
		dex.DefineStep(resolveSubmissionsDataSource{}),
		dex.DefineStep(notion.NewQueryDatabaseStep(notion.QueryDatabaseStepConfig[SubmissionRecord]{
			StepType: findExistingSubmissionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "notion", GroupLabel: "Notion",
				Explanation: "Query the data source for rows that already hold the submission ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindSubmissionRowsInput,
			Queried: sdkgo.GoTo(routeSubmission{}),
		})),
		dex.DefineStep(routeSubmission{}),
		dex.DefineStep(notion.NewCreatePageStep(notion.CreatePageStepConfig[SubmissionRecord]{
			StepType: createSubmissionRowStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "notion", GroupLabel: "Notion",
				Explanation: "Create the submission row with its properties and the message as the page body.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateSubmissionRowInput,
			Created:          sdkgo.GoTo(recordCreatedSubmission{}),
			Uncertain:        sdkgo.GoTo(recordUncertainSubmission{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedCreate{}),
		})),
		dex.DefineStep(recordCreatedSubmission{}),
		dex.DefineStep(recordUncertainSubmission{}),
		dex.DefineStep(notion.NewQueryDatabaseStep(notion.QueryDatabaseStepConfig[SubmissionRecord]{
			StepType: reconcileSubmissionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "notion", GroupLabel: "Notion",
				Explanation: "Query for the submission ID again to learn whether the uncertain create saved a row.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindSubmissionRowsInput,
			Queried: sdkgo.GoTo(routeReconciledSubmission{}),
		})),
		dex.DefineStep(routeReconciledSubmission{}),
		dex.DefineStep(notion.NewUpdatePagePropertiesStep(notion.UpdatePagePropertiesStepConfig[SubmissionRecord]{
			StepType: updateSubmissionRowStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "notion", GroupLabel: "Notion",
				Explanation: "Set the existing row's name, email, and submission time to the resubmitted values.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateSubmissionRowInput,
			Updated:          sdkgo.GoTo(recordUpdatedSubmission{}),
			ProviderRejected: sdkgo.GoTo(recordRejectedUpdate{}),
		})),
		dex.DefineStep(recordUpdatedSubmission{}),
		dex.DefineStep(notion.NewGetPageStep(notion.GetPageStepConfig[SubmissionRecord]{
			StepType: readBackSubmissionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "notion", GroupLabel: "Notion",
				Explanation: "Read the row back with its properties and page body as plain text.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReadBackSubmissionInput,
			Found: sdkgo.GoTo(completeSubmission{}),
		})),
		dex.DefineStep(completeSubmission{}),
		dex.DefineStep(recordRejectedCreate{}),
		dex.DefineStep(recordRejectedUpdate{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the submission Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{submissionAttribute}}
}

// GetDexSummary returns the submission state.
//
// dex:field attribute-key:notion-submission value-type:json editable:false description:"Submission and phase"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	record, err := submissionAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missingAttribute) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{submissionRecordAttributeKey: record}}, nil
}

// GetDexDisplay returns the submission state.
//
// dex:field attribute-key:notion-submission value-type:json editable:false description:"Submission, Notion row, and phase"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	record, err := submissionAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missingAttribute) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{submissionRecordAttributeKey: record}}, nil
}

// MapToFindSubmissionsDataSourceInput searches data sources whose title contains the database title.
func MapToFindSubmissionsDataSourceInput(record SubmissionRecord) notion.SearchInput {
	return notion.SearchInput{Query: record.Submission.DatabaseTitle, ObjectType: notion.SearchObjectDataSource, PageSize: searchPageSize}
}

// MapToFindSubmissionRowsInput reads up to two rows with the submission ID, enough to see a duplicate.
func MapToFindSubmissionRowsInput(record SubmissionRecord) notion.QueryDatabaseInput {
	return notion.QueryDatabaseInput{
		DataSourceID: record.DataSourceID,
		Filter: &notion.QueryFilter{
			Property: SubmissionIDProperty, Type: notion.PropertyTypeRichText, Condition: notion.FilterEquals, Text: record.Submission.SubmissionID,
		},
		Sorts:    []notion.QuerySort{{Timestamp: notion.TimestampCreatedTime, Direction: notion.SortAscending}},
		PageSize: duplicateDetectionPageSize,
	}
}

// MapToCreateSubmissionRowInput writes every submission property and the message as the page body.
func MapToCreateSubmissionRowInput(record SubmissionRecord) notion.CreatePageInput {
	properties := submissionProperties(record)
	properties[SubmissionIDProperty] = notion.RichTextValue(record.Submission.SubmissionID)
	return notion.CreatePageInput{DataSourceID: record.DataSourceID, Properties: properties, BodyText: record.Submission.Message}
}

// MapToUpdateSubmissionRowInput sets the resubmitted values with absolute writes; the body is left unchanged.
func MapToUpdateSubmissionRowInput(record SubmissionRecord) notion.UpdatePagePropertiesInput {
	return notion.UpdatePagePropertiesInput{PageID: record.PageID, Properties: submissionProperties(record)}
}

// MapToReadBackSubmissionInput reads the row and its page body.
func MapToReadBackSubmissionInput(record SubmissionRecord) notion.GetPageInput {
	return notion.GetPageInput{PageID: record.PageID, MaxTextCharacters: readBackMaximumTextCharacters}
}

func submissionProperties(record SubmissionRecord) map[string]notion.PropertyValue {
	return map[string]notion.PropertyValue{
		notion.TitlePropertyID: notion.TitleValue(record.Submission.Name),
		EmailProperty:          notion.EmailValue(record.Submission.Email),
		SubmittedAtProperty:    notion.DateValue(record.SubmittedAt),
	}
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Validate and record the submission before calling Notion."
type recordSubmission struct {
	dex.StepDefaults
}

func (recordSubmission) GetStepType() string { return recordSubmissionStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordSubmission) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordSubmission) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	submission, err := validateSubmission(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	record := SubmissionRecord{Submission: submission, SubmittedAt: ctx.FlowStartedAt().UTC().Format("2006-01-02T15:04:05Z"), Phase: PhaseRecorded}
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SubmissionRecord](findSubmissionsDataSourceStepType), record), nil
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Keep the one data source whose title equals the database title, or complete when none or several do."
type resolveSubmissionsDataSource struct {
	dex.StepDefaultsNoWaitFor[notion.SearchResult]
}

func (resolveSubmissionsDataSource) GetStepType() string { return resolveSubmissionsDataSourceType }

func (resolveSubmissionsDataSource) Execute(ctx dex.Context, result notion.SearchResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	candidateIDs := exactTitleMatchIDs(result.Value.Matches, record.Submission.DatabaseTitle)
	switch len(candidateIDs) {
	case 0:
		record.Phase = PhaseDataSourceNotFound
	case 1:
		record.Phase, record.DataSourceID = PhaseDataSourceResolved, candidateIDs[0]
	default:
		record.Phase, record.CandidateDataSourceIDs = PhaseDataSourceAmbiguous, candidateIDs
	}
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if record.Phase != PhaseDataSourceResolved {
		return dex.GracefulComplete(record), nil
	}
	return dex.GoTo(sdkgo.StepRef[SubmissionRecord](findExistingSubmissionStepType), record), nil
}

// exactTitleMatchIDs keeps data sources outside the trash whose title equals the wanted title, ignoring case.
func exactTitleMatchIDs(matches []notion.SearchMatch, title string) []string {
	var ids []string
	for _, match := range matches {
		if match.ObjectType == notion.SearchObjectDataSource && !match.IsInTrash && strings.EqualFold(strings.TrimSpace(match.Title), title) {
			ids = append(ids, match.ID)
		}
	}
	return ids
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Create a row when none holds the submission ID, update the one that does, or complete on duplicates."
type routeSubmission struct {
	dex.StepDefaultsNoWaitFor[notion.QueryDatabaseResult]
}

func (routeSubmission) GetStepType() string { return routeSubmissionStepType }

func (routeSubmission) Execute(ctx dex.Context, result notion.QueryDatabaseResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	pages := result.Value.Pages
	switch len(pages) {
	case 0:
		record.Phase = PhaseCreateRequested
	case 1:
		record.Phase, record.PageID, record.PageURL, record.IsExistingRow = PhaseUpdateRequested, pages[0].ID, pages[0].URL, true
	default:
		record.Phase, record.DuplicatePageIDs = PhaseDuplicateRows, pageIDs(pages)
	}
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	switch record.Phase {
	case PhaseCreateRequested:
		return dex.GoTo(sdkgo.StepRef[SubmissionRecord](createSubmissionRowStepType), record), nil
	case PhaseUpdateRequested:
		return dex.GoTo(sdkgo.StepRef[SubmissionRecord](updateSubmissionRowStepType), record), nil
	default:
		return dex.GracefulComplete(record), nil
	}
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Record the created row and read it back."
type recordCreatedSubmission struct {
	dex.StepDefaultsNoWaitFor[notion.CreatePageResult]
}

func (recordCreatedSubmission) GetStepType() string { return recordCreatedSubmissionStepType }

func (recordCreatedSubmission) Execute(ctx dex.Context, result notion.CreatePageResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.PageID = result.Value.PageID
	if result.Value.Page != nil {
		record.PageURL = result.Value.Page.URL
	}
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SubmissionRecord](readBackSubmissionStepType), record), nil
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Record why the create's outcome is unknown and query for the submission ID instead of creating again."
type recordUncertainSubmission struct {
	dex.StepDefaultsNoWaitFor[notion.CreatePageResult]
}

func (recordUncertainSubmission) GetStepType() string { return recordUncertainSubmissionStepType }

func (recordUncertainSubmission) Execute(ctx dex.Context, result notion.CreatePageResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.UncertainCreate = result.Failure
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SubmissionRecord](reconcileSubmissionStepType), record), nil
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Adopt the row the uncertain create saved, or complete as uncertain so an operator can re-run the same submission ID."
type routeReconciledSubmission struct {
	dex.StepDefaultsNoWaitFor[notion.QueryDatabaseResult]
}

func (routeReconciledSubmission) GetStepType() string { return routeReconciledSubmissionStepType }

func (routeReconciledSubmission) Execute(ctx dex.Context, result notion.QueryDatabaseResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	pages := result.Value.Pages
	switch len(pages) {
	case 0:
		record.Phase = PhaseCreateUncertain
	case 1:
		record.PageID, record.PageURL = pages[0].ID, pages[0].URL
	default:
		record.Phase, record.DuplicatePageIDs = PhaseDuplicateRows, pageIDs(pages)
	}
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	if len(pages) != 1 {
		return dex.GracefulComplete(record), nil
	}
	return dex.GoTo(sdkgo.StepRef[SubmissionRecord](readBackSubmissionStepType), record), nil
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Record the updated row and read it back."
type recordUpdatedSubmission struct {
	dex.StepDefaultsNoWaitFor[notion.UpdatePagePropertiesResult]
}

func (recordUpdatedSubmission) GetStepType() string { return recordUpdatedSubmissionStepType }

func (recordUpdatedSubmission) Execute(ctx dex.Context, result notion.UpdatePagePropertiesResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.PageURL = result.Value.Page.URL
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[SubmissionRecord](readBackSubmissionStepType), record), nil
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Confirm the row reads back with the submission ID, record its properties and body, and complete."
type completeSubmission struct {
	dex.StepDefaultsNoWaitFor[notion.GetPageResult]
}

func (completeSubmission) GetStepType() string { return completeSubmissionStepType }

func (completeSubmission) Execute(ctx dex.Context, result notion.GetPageResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	page := result.Value.Page
	if storedID := page.Properties[SubmissionIDProperty].PlainText; storedID != record.Submission.SubmissionID {
		return dex.ForceFail(fmt.Sprintf(submissionIDMismatchFailureMessage, page.ID, storedID)), nil
	}
	record.Phase = PhaseCreated
	if record.IsExistingRow {
		record.Phase = PhaseUpdated
	}
	record.PageURL = page.URL
	record.StoredProperties = make(map[string]string, len(page.Properties))
	for name, property := range page.Properties {
		record.StoredProperties[name] = property.PlainText
	}
	record.StoredBodyText = result.Value.Content.Text
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Record Notion's secret-safe rejection of the new row and complete."
type recordRejectedCreate struct {
	dex.StepDefaultsNoWaitFor[notion.CreatePageResult]
}

func (recordRejectedCreate) GetStepType() string { return recordRejectedCreateStepType }

func (recordRejectedCreate) Execute(ctx dex.Context, result notion.CreatePageResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Rejection = PhaseRejected, result.Failure
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

// dex:group group-id:notion group-label:"Notion"
// dex:explanation text:"Record Notion's secret-safe rejection of the row update and complete."
type recordRejectedUpdate struct {
	dex.StepDefaultsNoWaitFor[notion.UpdatePagePropertiesResult]
}

func (recordRejectedUpdate) GetStepType() string { return recordRejectedUpdateStepType }

func (recordRejectedUpdate) Execute(ctx dex.Context, result notion.UpdatePagePropertiesResult) (*dex.StepDecision, error) {
	record, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	record.Phase, record.Rejection = PhaseRejected, result.Failure
	if err := submissionAttribute.Set(ctx, record); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(record), nil
}

func pageIDs(pages []notion.Page) []string {
	ids := make([]string, 0, len(pages))
	for _, page := range pages {
		ids = append(ids, page.ID)
	}
	return ids
}

func validateSubmission(input Input) (Input, error) {
	submission := Input{
		DatabaseTitle: strings.TrimSpace(input.DatabaseTitle), SubmissionID: strings.TrimSpace(input.SubmissionID),
		Name: strings.TrimSpace(input.Name), Email: strings.TrimSpace(input.Email), Message: strings.TrimSpace(input.Message),
	}
	switch {
	case submission.DatabaseTitle == "" || utf8.RuneCountInString(submission.DatabaseTitle) > maximumDatabaseTitleRunes:
		return Input{}, fmt.Errorf("databaseTitle is the database's title, 1 to %d characters", maximumDatabaseTitleRunes)
	case submission.SubmissionID == "" || utf8.RuneCountInString(submission.SubmissionID) > maximumSubmissionIDRunes || strings.ContainsAny(submission.SubmissionID, "\r\n"):
		return Input{}, fmt.Errorf("submissionId is one line of 1 to %d characters", maximumSubmissionIDRunes)
	case submission.Name == "" || utf8.RuneCountInString(submission.Name) > maximumNameRunes:
		return Input{}, fmt.Errorf("name is 1 to %d characters", maximumNameRunes)
	case utf8.RuneCountInString(submission.Message) > maximumMessageRunes:
		return Input{}, fmt.Errorf("message is at most %d characters", maximumMessageRunes)
	}
	address, err := mail.ParseAddress(submission.Email)
	if err != nil || address.Address != submission.Email || len(submission.Email) > maximumEmailBytes {
		return Input{}, errors.New("email must be one plain email address")
	}
	return submission, nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
