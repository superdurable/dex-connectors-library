// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package responserecorder demonstrates every Google Forms operation in one
// Flow started from Dex Web Start Flow: read the picked form's questions, then
// record each new response's answers by question, either every response
// submitted since an earlier run's cursor or one response by ID.
package responserecorder

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "GoogleFormsResponseRecorder"
	// ConnectionName is the static Dex Web connection for Google Forms.
	ConnectionName = "google-forms-intake"
	// ResponsePageSize is how many responses each listResponses page requests.
	ResponsePageSize = 25
	// MaxResponsePages bounds one run; a listing with more pages completes as truncated.
	MaxResponsePages = 8
	// maxCursorResponseIDs bounds the response IDs a cursor carries for one submission instant.
	maxCursorResponseIDs = 1000

	recordRequestStepType  = "RecordRecorderRequest"
	readFormStepType       = "ReadForm"
	indexQuestionsStepType = "IndexFormQuestions"
	listResponsesStepType  = "ListNewResponses"
	recordPageStepType     = "RecordResponsePage"
	readResponseStepType   = "ReadNamedResponse"
	recordResponseStepType = "RecordNamedResponse"
	reportNotFoundStepType = "ReportResponseNotFound"
)

var (
	requestAttribute = dex.DefineAttribute[Input]("google-forms-recorder-request")
	formAttribute    = dex.DefineAttribute[RecordedForm]("google-forms-recorder-form")
	outcomeAttribute = dex.DefineAttribute[Outcome]("google-forms-recorder-outcome")
)

// Input is the typed request entered in Dex Web Start Flow. Leave both fields
// blank to record every response of the picked form.
type Input struct {
	// Cursor continues after an earlier run: pass that run's NextCursor to record
	// only the responses submitted since. Nil records every response.
	Cursor *ResponseCursor `json:"cursor,omitempty"`
	// ResponseID records only this response, read with getResponse, such as an ID
	// from an earlier run. Set it or Cursor, not both.
	ResponseID string `json:"responseId,omitempty"`
}

// ResponseCursor marks how far an earlier run recorded. Google filters by
// submission time, so several responses can share one instant; the cursor
// keeps their IDs to list "at or after" that time without recording them twice.
type ResponseCursor struct {
	// LastSubmittedAt is the latest submission time an earlier run recorded, in RFC 3339.
	LastSubmittedAt time.Time `json:"lastSubmittedAt"`
	// ResponseIDs are the responses an earlier run recorded at exactly LastSubmittedAt.
	ResponseIDs []string `json:"responseIds,omitempty"`
}

// FormConfiguration is the form the ReadForm Step's formPicker unit saves in Dex Web.
type FormConfiguration struct {
	// FormID is the picked form ID; blank means no form was chosen.
	FormID string `json:"formId,omitempty"`
	// FormTitle is the picked form's title, blank for a pasted form ID.
	FormTitle string `json:"formTitle,omitempty"`
}

// RecordedForm is the form's question index the Flow keeps while it records answers.
type RecordedForm struct {
	// FormID is the form ID.
	FormID string `json:"formId"`
	// Title is the form title.
	Title string `json:"title"`
	// Questions lists every question in form order.
	Questions []RecordedQuestion `json:"questions"`
}

// RecordedQuestion is one question's label and type.
type RecordedQuestion struct {
	// QuestionID keys the question's answers.
	QuestionID string `json:"questionId"`
	// Title is the question text; a grid row is "Grid prompt / Row".
	Title string `json:"title"`
	// Type is the Google Forms question type.
	Type forms.QuestionType `json:"type"`
}

// Status is the business outcome of one run.
type Status string

const (
	// StatusRecorded means every new response was recorded and NextCursor is current.
	StatusRecorded Status = "recorded"
	// StatusTruncated means more than MaxResponsePages pages matched; NextCursor stays at the input
	// cursor, because Google documents no listing order.
	StatusTruncated Status = "truncated"
	// StatusResponseNotFound means the requested response ID does not exist on the form.
	StatusResponseNotFound Status = "responseNotFound"
)

// Outcome is the durable result of the Flow and its completion output.
type Outcome struct {
	// Status is the business outcome.
	Status Status `json:"status,omitempty"`
	// FormID is the form whose responses were recorded.
	FormID string `json:"formId"`
	// FormTitle is the form title.
	FormTitle string `json:"formTitle,omitempty"`
	// Responses are the recorded responses in Google's listing order.
	Responses []RecordedResponse `json:"responses"`
	// PagesRead is the number of listResponses pages read.
	PagesRead int `json:"pagesRead,omitempty"`
	// NextCursor is the Cursor input for the next run; nil when nothing has been recorded yet.
	NextCursor *ResponseCursor `json:"nextCursor,omitempty"`
}

// RecordedResponse is one response with its answers in form order.
type RecordedResponse struct {
	// ResponseID is the Google Forms response ID.
	ResponseID string `json:"responseId"`
	// CreatedAt is when the response was first submitted.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// LastSubmittedAt is when the response was last submitted.
	LastSubmittedAt time.Time `json:"lastSubmittedAt"`
	// IsEdited reports a response the respondent submitted again after editing it.
	IsEdited bool `json:"isEdited,omitempty"`
	// RespondentEmail is the respondent's address when the form collects it.
	RespondentEmail string `json:"respondentEmail,omitempty"`
	// Answers are the answered questions in form order; an answer to a question the form no longer
	// has comes last with a blank title.
	Answers []RecordedAnswer `json:"answers"`
}

// RecordedAnswer is one answer labeled with its question.
type RecordedAnswer struct {
	// QuestionID is the answered question's ID.
	QuestionID string `json:"questionId"`
	// Question is the question text, blank when the form no longer has the question.
	Question string `json:"question,omitempty"`
	// Values are the text values of the answer.
	Values []string `json:"values,omitempty"`
	// FileNames are the names of files uploaded to a file upload question.
	FileNames []string `json:"fileNames,omitempty"`
}

// ResponsePageRequest is the application input of the listResponses Step.
type ResponsePageRequest struct {
	// FormID is the form whose responses are listed.
	FormID string `json:"formId"`
	// SubmittedAtOrAfter is the cursor time; zero lists every response.
	SubmittedAtOrAfter time.Time `json:"submittedAtOrAfter,omitzero"`
	// PageToken continues the listing; blank reads the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// Flow records the answers of a Google Form's new responses.
type Flow struct {
	dex.FlowDefaults
	connection forms.Connection
	form       sdkgo.ConnectorLoadedConfiguration[FormConfiguration]
}

// NewFlow binds the Google Forms Connection and the form picked in Dex Web,
// loaded at startup. A blank pick fails each run at its first Step.
func NewFlow(connection forms.Connection, form sdkgo.ConnectorLoadedConfiguration[FormConfiguration]) *Flow {
	return &Flow{connection: connection, form: form}
}

// FormConfigurationRef identifies the form pick of the ReadForm Step.
func FormConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: forms.ConnectorID, ConnectionName: ConnectionName, OperationID: "getForm",
		FlowType: FlowType, StepType: readFormStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, Google Forms, and recording Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordRecorderRequest{form: flow.form.Value}),
		dex.DefineStep(forms.NewGetFormStep(forms.GetFormStepConfig[Input]{
			StepType: readFormStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-forms", GroupLabel: "Google Forms",
				Explanation: "Read the picked form's questions so each answer can be labeled.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "form", UnitID: forms.UIUnitFormPicker, Label: "Form", Required: true,
				Description: "Choose the Google Form whose responses this Flow records; the picker lists the forms in the connected account's Google Drive and saves the form ID and title. The connected account must be able to edit the form to read its responses. Blank fails each run at its first Step.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: forms.UIFormPickerPortFormID, JSONPointer: "/formId"},
					{Port: forms.UIFormPickerPortFormTitle, JSONPointer: "/formTitle"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetFormInput,
			Found: sdkgo.GoTo(indexFormQuestions{}),
		})),
		dex.DefineStep(indexFormQuestions{}),
		dex.DefineStep(forms.NewListResponsesStep(forms.ListResponsesStepConfig[ResponsePageRequest]{
			StepType: listResponsesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-forms", GroupLabel: "Google Forms",
				Explanation: "List one page of the responses submitted at or after the cursor.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListResponsesInput,
			Listed: sdkgo.GoTo(recordResponsePage{}),
		})),
		dex.DefineStep(recordResponsePage{}),
		dex.DefineStep(forms.NewGetResponseStep(forms.GetResponseStepConfig[forms.GetResponseInput]{
			StepType: readResponseStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-forms", GroupLabel: "Google Forms",
				Explanation: "Read the one requested response by its ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetResponseInput,
			Found:    sdkgo.GoTo(recordNamedResponse{}),
			NotFound: sdkgo.GoTo(reportResponseNotFound{}),
		})),
		dex.DefineStep(recordNamedResponse{}),
		dex.DefineStep(reportResponseNotFound{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, form, and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, formAttribute, outcomeAttribute}}
}

// GetDexSummary returns the request and outcome.
//
// dex:field attribute-key:google-forms-recorder-request value-type:json editable:false description:"Requested cursor or response ID"
// dex:field attribute-key:google-forms-recorder-outcome value-type:json editable:false description:"Recorded responses and next cursor"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := recorderInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-forms-recorder-request": request,
		"google-forms-recorder-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the request and outcome.
//
// dex:field attribute-key:google-forms-recorder-request value-type:json editable:false description:"Cursor from an earlier run, or one response ID"
// dex:field attribute-key:google-forms-recorder-outcome value-type:json editable:false description:"Each recorded response's answers by question, and the cursor for the next run"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := recorderInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-forms-recorder-request": request,
		"google-forms-recorder-outcome": outcome,
	}}, nil
}

// MapToGetFormInput reads the form picked in Dex Web.
func (flow *Flow) MapToGetFormInput(Input) forms.GetFormInput {
	return forms.GetFormInput{FormID: flow.form.Value.FormID}
}

// MapToListResponsesInput lists one page at or after the cursor time.
func MapToListResponsesInput(request ResponsePageRequest) forms.ListResponsesInput {
	return forms.ListResponsesInput{
		FormID: request.FormID, SubmittedAtOrAfter: request.SubmittedAtOrAfter, PageSize: ResponsePageSize, PageToken: request.PageToken,
	}
}

// MapToGetResponseInput reads the requested response.
func MapToGetResponseInput(input forms.GetResponseInput) forms.GetResponseInput { return input }

// NextCursor returns the cursor after recording responses on top of previous:
// the latest submission time and every response ID at exactly that time.
func NextCursor(previous *ResponseCursor, responses []RecordedResponse) *ResponseCursor {
	var next *ResponseCursor
	if previous != nil {
		next = &ResponseCursor{LastSubmittedAt: previous.LastSubmittedAt, ResponseIDs: slices.Clone(previous.ResponseIDs)}
	}
	for _, response := range responses {
		switch {
		case next == nil || response.LastSubmittedAt.After(next.LastSubmittedAt):
			next = &ResponseCursor{LastSubmittedAt: response.LastSubmittedAt, ResponseIDs: []string{response.ResponseID}}
		case response.LastSubmittedAt.Equal(next.LastSubmittedAt) && !slices.Contains(next.ResponseIDs, response.ResponseID):
			next.ResponseIDs = append(next.ResponseIDs, response.ResponseID)
		}
	}
	return next
}

// RecordFormResponse labels one response's answers with the form's questions, in form order.
func RecordFormResponse(form RecordedForm, response forms.FormResponse) RecordedResponse {
	recorded := RecordedResponse{
		ResponseID: response.ResponseID, CreatedAt: response.CreatedAt, LastSubmittedAt: response.LastSubmittedAt,
		IsEdited:        !response.CreatedAt.IsZero() && response.LastSubmittedAt.After(response.CreatedAt),
		RespondentEmail: response.RespondentEmail, Answers: []RecordedAnswer{},
	}
	labeled := map[string]bool{}
	for _, question := range form.Questions {
		if answer, ok := response.AnswerByQuestionID(question.QuestionID); ok {
			recorded.Answers = append(recorded.Answers, recordAnswer(question.Title, answer))
			labeled[question.QuestionID] = true
		}
	}
	for _, answer := range response.Answers {
		if !labeled[answer.QuestionID] {
			recorded.Answers = append(recorded.Answers, recordAnswer("", answer))
		}
	}
	return recorded
}

func recordAnswer(question string, answer forms.FormAnswer) RecordedAnswer {
	recorded := RecordedAnswer{QuestionID: answer.QuestionID, Question: question, Values: answer.Values}
	for _, file := range answer.Files {
		recorded.FileNames = append(recorded.FileNames, file.FileName)
	}
	return recorded
}

func recorderInspection(ctx dex.Context) (Input, Outcome, error) {
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

// validateInput rejects a request the Flow cannot serve, before any provider call.
func validateInput(input Input, form FormConfiguration) string {
	switch {
	case strings.TrimSpace(form.FormID) == "":
		return "choose a form in the ReadForm Step's Form unit in Dex Web, then restart the Worker"
	case input.Cursor != nil && input.ResponseID != "":
		return "set cursor or responseId, not both"
	case input.Cursor != nil && input.Cursor.LastSubmittedAt.IsZero():
		return "cursor.lastSubmittedAt is required"
	case input.Cursor != nil && len(input.Cursor.ResponseIDs) > maxCursorResponseIDs:
		return "cursor.responseIds holds too many IDs"
	case input.ResponseID != "" && strings.TrimSpace(input.ResponseID) != input.ResponseID:
		return "responseId must not have surrounding spaces"
	}
	return ""
}

// dex:group group-id:response-recorder group-label:"Response recorder"
// dex:explanation text:"Validate the cursor or response ID and the picked form, and record the request."
type recordRecorderRequest struct {
	dex.StepDefaults
	form FormConfiguration
}

func (recordRecorderRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordRecorderRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordRecorderRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if reason := validateInput(input, step.form); reason != "" {
		return dex.ForceFail(reason), nil
	}
	if input.Cursor != nil {
		input.Cursor.LastSubmittedAt = input.Cursor.LastSubmittedAt.UTC()
	}
	if err := requestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readFormStepType), input), nil
}

// dex:group group-id:response-recorder group-label:"Response recorder"
// dex:explanation text:"Index the form's questions, then list new responses or read the one requested response."
type indexFormQuestions struct {
	dex.StepDefaultsNoWaitFor[forms.GetFormResult]
}

func (indexFormQuestions) GetStepType() string { return indexQuestionsStepType }

func (indexFormQuestions) Execute(ctx dex.Context, result forms.GetFormResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	form := RecordedForm{FormID: result.Value.FormID, Title: result.Value.Title, Questions: []RecordedQuestion{}}
	for _, item := range result.Value.Items {
		for _, question := range item.Questions {
			title := question.Title
			if item.Kind == forms.FormItemKindQuestionGroup {
				title = item.Title + " / " + question.Title
			}
			form.Questions = append(form.Questions, RecordedQuestion{QuestionID: question.QuestionID, Title: title, Type: question.Type})
		}
	}
	if err := formAttribute.Set(ctx, form); err != nil {
		return nil, err
	}
	if err := outcomeAttribute.Set(ctx, Outcome{FormID: form.FormID, FormTitle: form.Title, Responses: []RecordedResponse{}}); err != nil {
		return nil, err
	}
	if request.ResponseID != "" {
		return dex.GoTo(sdkgo.StepRef[forms.GetResponseInput](readResponseStepType),
			forms.GetResponseInput{FormID: form.FormID, ResponseID: request.ResponseID}), nil
	}
	pageRequest := ResponsePageRequest{FormID: form.FormID}
	if request.Cursor != nil {
		pageRequest.SubmittedAtOrAfter = request.Cursor.LastSubmittedAt
	}
	return dex.GoTo(sdkgo.StepRef[ResponsePageRequest](listResponsesStepType), pageRequest), nil
}

// dex:group group-id:response-recorder group-label:"Response recorder"
// dex:explanation text:"Record the page's new responses, then read the next page or complete with the next cursor."
type recordResponsePage struct {
	dex.StepDefaultsNoWaitFor[forms.ListResponsesResult]
}

func (recordResponsePage) GetStepType() string { return recordPageStepType }

func (recordResponsePage) Execute(ctx dex.Context, result forms.ListResponsesResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	form, err := formAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	for _, response := range result.Value.Responses {
		if isRecordedBefore(request.Cursor, response) || slices.ContainsFunc(outcome.Responses, func(recorded RecordedResponse) bool {
			return recorded.ResponseID == response.ResponseID
		}) {
			continue
		}
		outcome.Responses = append(outcome.Responses, RecordFormResponse(form, response))
	}
	outcome.PagesRead++
	switch {
	case result.Value.NextPageToken != "" && outcome.PagesRead < MaxResponsePages:
		if err := outcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		next := ResponsePageRequest{FormID: form.FormID, PageToken: result.Value.NextPageToken}
		if request.Cursor != nil {
			next.SubmittedAtOrAfter = request.Cursor.LastSubmittedAt
		}
		return dex.GoTo(sdkgo.StepRef[ResponsePageRequest](listResponsesStepType), next), nil
	case result.Value.NextPageToken != "":
		// Google documents no listing order, so an unread page may hold an earlier response.
		outcome.Status, outcome.NextCursor = StatusTruncated, request.Cursor
	default:
		outcome.Status, outcome.NextCursor = StatusRecorded, NextCursor(request.Cursor, outcome.Responses)
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// isRecordedBefore reports a response an earlier run recorded, or one older than the cursor.
func isRecordedBefore(cursor *ResponseCursor, response forms.FormResponse) bool {
	if cursor == nil {
		return false
	}
	return response.LastSubmittedAt.Before(cursor.LastSubmittedAt) ||
		(response.LastSubmittedAt.Equal(cursor.LastSubmittedAt) && slices.Contains(cursor.ResponseIDs, response.ResponseID))
}

// dex:group group-id:response-recorder group-label:"Response recorder"
// dex:explanation text:"Record the requested response's answers and complete the Flow."
type recordNamedResponse struct {
	dex.StepDefaultsNoWaitFor[forms.GetResponseResult]
}

func (recordNamedResponse) GetStepType() string { return recordResponseStepType }

func (recordNamedResponse) Execute(ctx dex.Context, result forms.GetResponseResult) (*dex.StepDecision, error) {
	form, err := formAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Status = StatusRecorded
	outcome.Responses = []RecordedResponse{RecordFormResponse(form, result.Value)}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:response-recorder group-label:"Response recorder"
// dex:explanation text:"Complete with responseNotFound when the form has no response with the requested ID."
type reportResponseNotFound struct {
	dex.StepDefaultsNoWaitFor[forms.GetResponseResult]
}

func (reportResponseNotFound) GetStepType() string { return reportNotFoundStepType }

func (reportResponseNotFound) Execute(ctx dex.Context, _ forms.GetResponseResult) (*dex.StepDecision, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Status = StatusResponseNotFound
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
