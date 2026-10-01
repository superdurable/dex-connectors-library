// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package policypublish demonstrates every Google Docs operation in one Flow
// started from Dex Web Start Flow: read a policy template, create a new
// document from it, fill its placeholders at the revision just read, append a
// publication stamp, and read the published text back.
package policypublish

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "GoogleDocsPolicyPublish"
	// ConnectionName is the static Dex Web connection for Google Docs.
	ConnectionName = "google-docs-policies"

	recordRequestStepType      = "RecordPublishRequest"
	readTemplateStepType       = "ReadPolicyTemplate"
	prepareDraftStepType       = "PreparePolicyDraft"
	createDocumentStepType     = "CreatePolicyDocument"
	readDraftStepType          = "ReadPolicyDraft"
	prepareFillStepType        = "PreparePlaceholderFill"
	fillPlaceholdersStepType   = "FillPolicyPlaceholders"
	prepareStampStepType       = "PreparePublicationStamp"
	appendStampStepType        = "AppendPublicationStamp"
	readBackStepType           = "ReadBackPolicy"
	completeStepType           = "CompletePublication"
	reportUncertainStepType    = "ReportCreationUncertain"
	reportMissingStepType      = "ReportPlaceholderMissing"
	publicationStampDateLayout = "2006-01-02"
)

var (
	requestAttribute      = dex.DefineAttribute[Input]("google-docs-policy-publish-request")
	outcomeAttribute      = dex.DefineAttribute[Outcome]("google-docs-policy-publish-outcome")
	createResultAttribute = dex.DefineAttribute[docs.CreateDocumentResult]("google-docs-policy-publish-create-result")
	appendResultAttribute = dex.DefineAttribute[docs.AppendTextResult]("google-docs-policy-publish-append-result")
)

// Input is the typed request entered in Dex Web Start Flow.
type Input struct {
	// Title is the published document's title, such as Acme Refund Policy 2026.
	Title string `json:"title"`
	// Placeholders lists each template placeholder and its value, such as
	// {{effectiveDate}} and 2026-10-01. Every placeholder must occur in the template.
	Placeholders []docs.PlaceholderReplacement `json:"placeholders"`
}

// TemplateConfiguration is the template a documentPicker unit saves in Dex Web.
type TemplateConfiguration struct {
	// DocumentID is the template's Google Docs document ID.
	DocumentID string `json:"documentId,omitempty"`
	// DocumentTitle is the template's title when it was picked from the list.
	DocumentTitle string `json:"documentTitle,omitempty"`
}

// FolderConfiguration is the folder a folderPicker unit saves in Dex Web.
type FolderConfiguration struct {
	// FolderID is the destination Drive folder ID; blank means the My Drive root.
	FolderID string `json:"folderId,omitempty"`
	// FolderName is the folder's display name when it was picked from the list.
	FolderName string `json:"folderName,omitempty"`
}

// Status is the business outcome of one publication.
type Status string

const (
	// StatusPublished means the document was created, filled, stamped, and read back.
	StatusPublished Status = "published"
	// StatusCreationUncertain means Google may have created the document; a person must check Drive.
	StatusCreationUncertain Status = "creationUncertain"
	// StatusPlaceholderMissing means the template lacks a requested placeholder, so nothing was filled.
	StatusPlaceholderMissing Status = "placeholderMissing"
)

// Outcome is the durable result of the Flow and its completion output.
type Outcome struct {
	// Status is the business outcome.
	Status Status `json:"status"`
	// TemplateDocumentID is the template that was read.
	TemplateDocumentID string `json:"templateDocumentId,omitempty"`
	// TemplateRevisionID is the template revision the published document copies.
	TemplateRevisionID string `json:"templateRevisionId,omitempty"`
	// Document is the created document.
	Document *docs.CreatedDocument `json:"document,omitempty"`
	// IsCreationFromEarlierAttempt reports that a retried create found the document an earlier attempt created.
	IsCreationFromEarlierAttempt bool `json:"isCreationFromEarlierAttempt,omitempty"`
	// WasFillAlreadyApplied reports that a repeated fill found the placeholders an earlier attempt replaced.
	WasFillAlreadyApplied bool `json:"wasFillAlreadyApplied,omitempty"`
	// WasStampAlreadyApplied reports that a repeated append found the stamp an earlier attempt added.
	WasStampAlreadyApplied bool `json:"wasStampAlreadyApplied,omitempty"`
	// MissingPlaceholders lists the requested placeholders the template lacks.
	MissingPlaceholders []string `json:"missingPlaceholders,omitempty"`
	// PublishedRevisionID is the revision of the text read back.
	PublishedRevisionID string `json:"publishedRevisionId,omitempty"`
	// PublishedText is the Markdown read back from the published document.
	PublishedText string `json:"publishedText,omitempty"`
	// RemainingPlaceholders lists requested placeholders still present in the read-back text.
	RemainingPlaceholders []string `json:"remainingPlaceholders,omitempty"`
	// ReviewDetail is the connector's safe failure message for a person to act on.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// DraftRequest is the application input of the create Step.
type DraftRequest struct {
	// Title is the new document's title.
	Title string `json:"title"`
	// TemplateMarkdown is the template's Markdown, placeholders included.
	TemplateMarkdown string `json:"templateMarkdown"`
}

// FillRequest is the application input of the placeholder fill Step.
type FillRequest struct {
	// DocumentID is the created document.
	DocumentID string `json:"documentId"`
	// RevisionID is the created document's revision that was read.
	RevisionID string `json:"revisionId"`
	// Placeholders lists the requested replacements.
	Placeholders []docs.PlaceholderReplacement `json:"placeholders"`
}

// StampRequest is the application input of the append Step.
type StampRequest struct {
	// DocumentID is the filled document.
	DocumentID string `json:"documentId"`
	// RevisionID is the revision the fill produced.
	RevisionID string `json:"revisionId"`
	// Text is the publication stamp.
	Text string `json:"text"`
}

// Flow publishes one policy document from a Google Docs template.
type Flow struct {
	dex.FlowDefaults
	connection        docs.Connection
	template          sdkgo.ConnectorLoadedConfiguration[TemplateConfiguration]
	destinationFolder sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]
}

// NewFlow binds the Google Docs Connection and the picks loaded at startup.
// The template is required; a blank destination folder publishes to the My Drive root.
func NewFlow(
	connection docs.Connection,
	template sdkgo.ConnectorLoadedConfiguration[TemplateConfiguration],
	destinationFolder sdkgo.ConnectorLoadedConfiguration[FolderConfiguration],
) *Flow {
	return &Flow{connection: connection, template: template, destinationFolder: destinationFolder}
}

// TemplateConfigurationRef identifies the template pick of the template read Step.
func TemplateConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: docs.ConnectorID, ConnectionName: ConnectionName, OperationID: "getDocumentText",
		FlowType: FlowType, StepType: readTemplateStepType,
	}
}

// DestinationFolderConfigurationRef identifies the folder pick of the create Step.
func DestinationFolderConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: docs.ConnectorID, ConnectionName: ConnectionName, OperationID: "createDocument",
		FlowType: FlowType, StepType: createDocumentStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, Google Docs, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordPublishRequest{flow: flow}),
		dex.DefineStep(docs.NewGetDocumentTextStep(docs.GetDocumentTextStepConfig[Input]{
			StepType: readTemplateStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-docs", GroupLabel: "Google Docs",
				Explanation: "Read the policy template as Markdown, without pending suggestions.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "policyTemplate", UnitID: docs.UIUnitDocumentPicker, Label: "Policy template", Required: true,
				Description: "Choose the Google Doc whose text, including its {{placeholders}}, every published policy copies; the Flow fails at its first Step until a template is saved.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: docs.UIDocumentPickerPortDocumentID, JSONPointer: "/documentId"},
					{Port: docs.UIDocumentPickerPortDocumentTitle, JSONPointer: "/documentTitle"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToReadTemplateInput,
			Read: sdkgo.GoTo(preparePolicyDraft{}),
		})),
		dex.DefineStep(preparePolicyDraft{}),
		dex.DefineStep(docs.NewCreateDocumentStep(docs.CreateDocumentStepConfig[DraftRequest]{
			StepType: createDocumentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-docs", GroupLabel: "Google Docs",
				Explanation: "Create the policy document from the template once, reusing the one an earlier attempt created.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "destinationFolder", UnitID: docs.UIUnitFolderPicker, Label: "Destination folder",
				Description: "Choose the Drive folder that receives each published policy; leave blank to create it in the My Drive root.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: docs.UIFolderPickerPortFolderID, JSONPointer: "/folderId"},
					{Port: docs.UIFolderPickerPortFolderName, JSONPointer: "/folderName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToCreateDocumentInput,
			Created:         sdkgo.GoTo(sdkgo.StepRef[docs.CreateDocumentResult](readDraftStepType)),
			Uncertain:       sdkgo.GoTo(reportCreationUncertain{}),
			ResultAttribute: &createResultAttribute,
		})),
		dex.DefineStep(docs.NewGetDocumentTextStep(docs.GetDocumentTextStepConfig[docs.CreateDocumentResult]{
			StepType: readDraftStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-docs", GroupLabel: "Google Docs",
				Explanation: "Read the new document's revision so the fill applies only to this exact text.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReadDraftInput,
			Read: sdkgo.GoTo(preparePlaceholderFill{}),
		})),
		dex.DefineStep(preparePlaceholderFill{}),
		dex.DefineStep(docs.NewReplaceDocumentTextStep(docs.ReplaceDocumentTextStepConfig[FillRequest]{
			StepType: fillPlaceholdersStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-docs", GroupLabel: "Google Docs",
				Explanation: "Replace every placeholder in one batch that Google applies only at the revision read.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFillPlaceholdersInput,
			Replaced:            sdkgo.GoTo(preparePublicationStamp{}),
			PlaceholderNotFound: sdkgo.GoTo(reportPlaceholderMissing{}),
		})),
		dex.DefineStep(preparePublicationStamp{}),
		dex.DefineStep(docs.NewAppendTextStep(docs.AppendTextStepConfig[StampRequest]{
			StepType: appendStampStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-docs", GroupLabel: "Google Docs",
				Explanation: "Append the publication stamp once, at the revision the fill produced.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAppendStampInput,
			Appended:        sdkgo.GoTo(sdkgo.StepRef[docs.AppendTextResult](readBackStepType)),
			ResultAttribute: &appendResultAttribute,
		})),
		dex.DefineStep(docs.NewGetDocumentTextStep(docs.GetDocumentTextStepConfig[docs.AppendTextResult]{
			StepType: readBackStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-docs", GroupLabel: "Google Docs",
				Explanation: "Read the published document back as Markdown to confirm its text.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReadBackInput,
			Read: sdkgo.GoTo(completePublication{}),
		})),
		dex.DefineStep(completePublication{}),
		dex.DefineStep(reportCreationUncertain{}),
		dex.DefineStep(reportPlaceholderMissing{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, outcome, and connector result Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, outcomeAttribute, createResultAttribute, appendResultAttribute}}
}

// GetDexSummary returns the request and outcome.
//
// dex:field attribute-key:google-docs-policy-publish-request value-type:json editable:false description:"Requested title and placeholder values"
// dex:field attribute-key:google-docs-policy-publish-outcome value-type:json editable:false description:"Publication outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := publicationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-docs-policy-publish-request": request,
		"google-docs-policy-publish-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the request and outcome.
//
// dex:field attribute-key:google-docs-policy-publish-request value-type:json editable:false description:"Document title and placeholder values"
// dex:field attribute-key:google-docs-policy-publish-outcome value-type:json editable:false description:"Template revision, created document, and published text"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := publicationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-docs-policy-publish-request": request,
		"google-docs-policy-publish-outcome": outcome,
	}}, nil
}

// MapToReadTemplateInput reads the saved template as Markdown.
func (flow *Flow) MapToReadTemplateInput(Input) docs.GetDocumentTextInput {
	return docs.GetDocumentTextInput{DocumentID: flow.template.Value.DocumentID, Format: docs.TextFormatMarkdown}
}

// MapToCreateDocumentInput imports the template Markdown into the saved destination folder.
func (flow *Flow) MapToCreateDocumentInput(request DraftRequest) docs.CreateDocumentInput {
	return docs.CreateDocumentInput{
		Title: request.Title, ParentFolderID: flow.destinationFolder.Value.FolderID,
		InitialText: request.TemplateMarkdown, InitialTextFormat: docs.TextFormatMarkdown,
	}
}

// MapToReadDraftInput reads the created document.
func MapToReadDraftInput(result docs.CreateDocumentResult) docs.GetDocumentTextInput {
	return docs.GetDocumentTextInput{DocumentID: result.Value.Document.DocumentID, Format: docs.TextFormatPlainText}
}

// MapToFillPlaceholdersInput fills the placeholders at the revision that was read.
func MapToFillPlaceholdersInput(request FillRequest) docs.ReplaceDocumentTextInput {
	return docs.ReplaceDocumentTextInput{
		DocumentID: request.DocumentID, RequiredRevisionID: request.RevisionID,
		Target: docs.ReplaceTargetPlaceholders, Placeholders: request.Placeholders,
	}
}

// MapToAppendStampInput appends the stamp at the revision the fill produced.
func MapToAppendStampInput(request StampRequest) docs.AppendTextInput {
	return docs.AppendTextInput{DocumentID: request.DocumentID, RequiredRevisionID: request.RevisionID, Text: request.Text}
}

// MapToReadBackInput reads the published document as Markdown.
func MapToReadBackInput(result docs.AppendTextResult) docs.GetDocumentTextInput {
	return docs.GetDocumentTextInput{DocumentID: result.Value.DocumentID, Format: docs.TextFormatMarkdown}
}

func publicationInspection(ctx dex.Context) (Input, Outcome, error) {
	request, err := optionalAttribute(ctx, requestAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	outcome, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:policy-publish group-label:"Policy publication"
// dex:explanation text:"Validate and record the requested title and placeholder values."
type recordPublishRequest struct {
	dex.StepDefaults
	flow *Flow
}

func (recordPublishRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordPublishRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordPublishRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" || len(input.Placeholders) == 0 {
		return dex.ForceFail("title and at least one placeholder are required"), nil
	}
	if step.flow.template.Value.DocumentID == "" {
		return dex.ForceFail("choose a policy template on the ReadPolicyTemplate Step in Dex Web, then restart the Worker"), nil
	}
	if err := requestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readTemplateStepType), input), nil
}

// dex:group group-id:policy-publish group-label:"Policy publication"
// dex:explanation text:"Record the template revision and prepare the new document from its Markdown."
type preparePolicyDraft struct {
	dex.StepDefaultsNoWaitFor[docs.GetDocumentTextResult]
}

func (preparePolicyDraft) GetStepType() string { return prepareDraftStepType }

func (preparePolicyDraft) Execute(ctx dex.Context, result docs.GetDocumentTextResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := Outcome{TemplateDocumentID: result.Value.DocumentID, TemplateRevisionID: result.Value.RevisionID}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	draft := DraftRequest{Title: request.Title, TemplateMarkdown: result.Value.Text}
	return dex.GoTo(sdkgo.StepRef[DraftRequest](createDocumentStepType), draft), nil
}

// dex:group group-id:policy-publish group-label:"Policy publication"
// dex:explanation text:"Pair the new document's revision with the requested placeholder values."
type preparePlaceholderFill struct {
	dex.StepDefaultsNoWaitFor[docs.GetDocumentTextResult]
}

func (preparePlaceholderFill) GetStepType() string { return prepareFillStepType }

func (preparePlaceholderFill) Execute(ctx dex.Context, result docs.GetDocumentTextResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	fill := FillRequest{DocumentID: result.Value.DocumentID, RevisionID: result.Value.RevisionID, Placeholders: request.Placeholders}
	return dex.GoTo(sdkgo.StepRef[FillRequest](fillPlaceholdersStepType), fill), nil
}

// dex:group group-id:policy-publish group-label:"Policy publication"
// dex:explanation text:"Record the fill and prepare the publication stamp at the revision it produced."
type preparePublicationStamp struct {
	dex.StepDefaultsNoWaitFor[docs.ReplaceDocumentTextResult]
}

func (preparePublicationStamp) GetStepType() string { return prepareStampStepType }

func (preparePublicationStamp) Execute(ctx dex.Context, result docs.ReplaceDocumentTextResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.WasFillAlreadyApplied = result.Value.WasAlreadyApplied
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	stamp := StampRequest{
		DocumentID: result.Value.DocumentID, RevisionID: result.Value.RevisionID,
		Text: PublicationStampText(ctx.FlowStartedAt().UTC().Format(publicationStampDateLayout), outcome.TemplateRevisionID),
	}
	return dex.GoTo(sdkgo.StepRef[StampRequest](appendStampStepType), stamp), nil
}

// PublicationStampText is the line appended to every published policy.
func PublicationStampText(publishedOn string, templateRevisionID string) string {
	return "Published by Dex on " + publishedOn + " from template revision " + templateRevisionID + "."
}

// dex:group group-id:policy-publish group-label:"Policy publication"
// dex:explanation text:"Record the published text and complete the Flow."
type completePublication struct {
	dex.StepDefaultsNoWaitFor[docs.GetDocumentTextResult]
}

func (completePublication) GetStepType() string { return completeStepType }

func (completePublication) Execute(ctx dex.Context, result docs.GetDocumentTextResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	createResult, err := createResultAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	appendResult, err := appendResultAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	document := createResult.Value.Document
	outcome.Status = StatusPublished
	outcome.Document = &document
	outcome.IsCreationFromEarlierAttempt = createResult.Value.IsFromEarlierAttempt
	outcome.WasStampAlreadyApplied = appendResult.Value.WasAlreadyApplied
	outcome.PublishedRevisionID = result.Value.RevisionID
	outcome.PublishedText = result.Value.Text
	outcome.RemainingPlaceholders = RemainingPlaceholders(result.Value.Text, request.Placeholders)
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// RemainingPlaceholders lists the requested placeholders that text still contains.
func RemainingPlaceholders(text string, placeholders []docs.PlaceholderReplacement) []string {
	var remaining []string
	for _, placeholder := range placeholders {
		if strings.Contains(text, placeholder.Placeholder) {
			remaining = append(remaining, placeholder.Placeholder)
		}
	}
	return remaining
}

// dex:group group-id:policy-publish group-label:"Policy publication"
// dex:explanation text:"Complete with creationUncertain so a person checks Drive instead of creating a second document."
type reportCreationUncertain struct {
	dex.StepDefaultsNoWaitFor[docs.CreateDocumentResult]
}

func (reportCreationUncertain) GetStepType() string { return reportUncertainStepType }

func (reportCreationUncertain) Execute(ctx dex.Context, result docs.CreateDocumentResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Status = StatusCreationUncertain
	if result.Failure != nil {
		outcome.ReviewDetail = result.Failure.Message
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:policy-publish group-label:"Policy publication"
// dex:explanation text:"Complete with placeholderMissing when the template lacks a requested placeholder."
type reportPlaceholderMissing struct {
	dex.StepDefaultsNoWaitFor[docs.ReplaceDocumentTextResult]
}

func (reportPlaceholderMissing) GetStepType() string { return reportMissingStepType }

func (reportPlaceholderMissing) Execute(ctx dex.Context, result docs.ReplaceDocumentTextResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	createResult, err := createResultAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	document := createResult.Value.Document
	outcome.Status = StatusPlaceholderMissing
	outcome.Document = &document
	outcome.MissingPlaceholders = result.Value.MissingPlaceholders
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
