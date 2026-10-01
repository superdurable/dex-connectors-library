// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package responserecorder starts one Flow per submitted Typeform response from the responseSubmitted
// Trigger, records its typed answers and hidden fields, reads the form back with getForm, and records
// every question with its answer, including the questions the respondent skipped.
package responserecorder

import (
	"errors"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "TypeformResponseRecorder"
	// ConnectionName is the static Dex Web connection that verifies webhooks and reads forms.
	ConnectionName = "typeform-forms"
	// ResponseSubmittedTriggerBinding is the responseSubmitted binding whose submissions start this Flow.
	ResponseSubmittedTriggerBinding = "response-submitted"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered submission maps to one Flow.
	FlowIDPrefix = "typeform-"

	readFormStepType = "ReadForm"
)

var (
	submissionAttribute = dex.DefineAttribute[Submission]("typeform-submission")
	questionsAttribute  = dex.DefineAttribute[RecordedQuestions]("typeform-questions")
)

// Submission is the Flow's start input: one verified form_response webhook.
type Submission struct {
	// EventID is the Trigger event ID, <form id>:<response token>.
	EventID string `json:"eventId"`
	// FormID is the submitted form's ID.
	FormID string `json:"formId"`
	// FormTitle is the form's title.
	FormTitle string `json:"formTitle,omitempty"`
	// ResponseToken is Typeform's response ID.
	ResponseToken string `json:"responseToken"`
	// SubmittedAt is when the respondent submitted the form, in UTC.
	SubmittedAt time.Time `json:"submittedAt"`
	// Answers are the typed answers, each keyed by its field ID and ref.
	Answers []typeform.FormAnswer `json:"answers"`
	// HiddenFields maps each hidden field name to its value.
	HiddenFields map[string]string `json:"hiddenFields,omitempty"`
}

// RecordedQuestions is the getForm outcome the Flow stores and completes with on the found branch.
type RecordedQuestions struct {
	// Branch is the getForm branch: found, notFound, providerRejected, invalidResponse, or defect.
	Branch sdkgo.BranchID `json:"branch"`
	// Questions pairs every question of the form, in form order, with its answer.
	Questions []RecordedQuestion `json:"questions,omitempty"`
	// UnmatchedAnswers answer fields the form no longer has, because it changed after the submission.
	UnmatchedAnswers []typeform.FormAnswer `json:"unmatchedAnswers,omitempty"`
	// FailureKind is the safe failure category of a branch other than found.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// FailureMessage is the safe failure message, which never holds tokens or Typeform response text.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// RecordedQuestion is one question of the form and the submission's answer to it.
type RecordedQuestion struct {
	// Field is the question as getForm read it, with its ID, ref, type, and title.
	Field typeform.FormField `json:"field"`
	// Answer is the respondent's answer; nil means the respondent skipped the question.
	Answer *typeform.FormAnswer `json:"answer,omitempty"`
}

// Flow records one submitted Typeform response question by question.
type Flow struct {
	dex.FlowDefaults
	connection typeform.Connection
}

// NewFlow binds the Typeform Connection that reads the submitted form.
func NewFlow(connection typeform.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the record, read, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSubmission{}),
		dex.DefineStep(typeform.NewGetFormStep(typeform.GetFormStepConfig[Submission]{
			StepType: readFormStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "typeform", GroupLabel: "Typeform",
				Explanation: "Read the submitted form's questions, with their field IDs and refs, back from Typeform.",
			},
			Connection:          flow.connection,
			MapToOperationInput: MapToGetFormInput,
			Found:               sdkgo.GoTo(recordQuestions{}),
			NotFound:            sdkgo.GoTo(recordReadFailure{}),
			ProviderRejected:    sdkgo.GoTo(recordReadFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordReadFailure{}),
			Defect:              sdkgo.GoTo(recordReadFailure{}),
		})),
		dex.DefineStep(recordQuestions{}),
		dex.DefineStep(recordReadFailure{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the submission and question Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{submissionAttribute, questionsAttribute}}
}

// GetConnectorTriggerBindings declares the responseSubmitted binding that starts this Flow.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		typeform.DefineResponseSubmittedTriggerBinding(typeform.ResponseSubmittedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: ResponseSubmittedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "form", UnitID: typeform.UIUnitFormPicker, Label: "Form",
				Description: "Select the Typeform form whose submissions start this Flow; the picker lists the connected account's forms and stores the form ID. Leave it empty to record submissions of every form whose webhook points at this application.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: typeform.UIFormPickerPortFormID, JSONPointer: "/formId"}},
			}}},
		}),
	}
}

// GetDexSummary returns the submission and its questions for the Dex Web run list.
//
// dex:field attribute-key:typeform-submission value-type:json editable:false description:"Submitted Typeform response"
// dex:field attribute-key:typeform-questions value-type:json editable:false description:"Questions paired with their answers"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	submission, recorded, err := submissionInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"typeform-submission": submission, "typeform-questions": recorded}}, nil
}

// GetDexDisplay returns the submission and its questions for the Dex Web run detail.
//
// dex:field attribute-key:typeform-submission value-type:json editable:false description:"Form, response token, typed answers, and hidden fields"
// dex:field attribute-key:typeform-questions value-type:json editable:false description:"getForm branch and every question with its answer"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	submission, recorded, err := submissionInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"typeform-submission": submission, "typeform-questions": recorded}}, nil
}

// AcceptSubmission is the application's admission rule: a submission without any answer starts no Flow.
func AcceptSubmission(event sdkgo.TriggerEvent[typeform.FormResponseEvent]) bool {
	return len(event.Payload.Response.Answers) > 0
}

// ResolveFlowID derives the Flow ID from the event ID, so every redelivery maps to one Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[typeform.FormResponseEvent]) string {
	return FlowIDPrefix + strings.ReplaceAll(event.ID, ":", "-")
}

// MapToFlowInput copies the verified submission into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[typeform.FormResponseEvent]) Submission {
	response := event.Payload.Response
	return Submission{
		EventID: event.ID, FormID: event.Payload.FormID, FormTitle: event.Payload.FormTitle, ResponseToken: response.Token,
		SubmittedAt: response.SubmittedAt, Answers: response.Answers, HiddenFields: response.HiddenFields,
	}
}

// MapToGetFormInput reads the form the respondent submitted.
func MapToGetFormInput(submission Submission) typeform.GetFormInput {
	return typeform.GetFormInput{FormID: submission.FormID}
}

// PairQuestionsWithAnswers lists every form field with the submission's answer to it, matched by field ID
// because Typeform's generated refs are not persistent, and returns the answers no field matches.
func PairQuestionsWithAnswers(form typeform.Form, submission Submission) ([]RecordedQuestion, []typeform.FormAnswer) {
	answersByFieldID := make(map[string]typeform.FormAnswer, len(submission.Answers))
	for _, answer := range submission.Answers {
		answersByFieldID[answer.FieldID] = answer
	}
	questions := make([]RecordedQuestion, 0, len(form.Fields))
	for _, field := range form.Fields {
		question := RecordedQuestion{Field: field}
		if answer, isAnswered := answersByFieldID[field.ID]; isAnswered {
			question.Answer = &answer
			delete(answersByFieldID, field.ID)
		}
		questions = append(questions, question)
	}
	var unmatched []typeform.FormAnswer
	for _, answer := range submission.Answers {
		if _, isUnmatched := answersByFieldID[answer.FieldID]; isUnmatched {
			unmatched = append(unmatched, answer)
		}
	}
	return questions, unmatched
}

func submissionInspection(ctx dex.Context) (Submission, RecordedQuestions, error) {
	submission, err := optionalAttribute(ctx, submissionAttribute)
	if err != nil {
		return Submission{}, RecordedQuestions{}, err
	}
	recorded, err := optionalAttribute(ctx, questionsAttribute)
	if err != nil {
		return Submission{}, RecordedQuestions{}, err
	}
	return submission, recorded, nil
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

// dex:group group-id:submission group-label:"Submission"
// dex:explanation text:"Persist the verified submission's typed answers and hidden fields before reading its form."
type recordSubmission struct {
	dex.StepDefaultsNoWaitFor[Submission]
}

func (recordSubmission) GetStepType() string { return "RecordSubmission" }

func (recordSubmission) Execute(ctx dex.Context, submission Submission) (*dex.StepDecision, error) {
	if submission.EventID == "" || submission.FormID == "" || submission.ResponseToken == "" {
		return dex.ForceFail("a submission requires its event ID, form ID, and response token"), nil
	}
	if err := submissionAttribute.Set(ctx, submission); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Submission](readFormStepType), submission), nil
}

// dex:group group-id:typeform group-label:"Typeform"
// dex:explanation text:"Pair every question of the form with the recorded answer, persist them, and complete the Flow."
type recordQuestions struct {
	dex.StepDefaultsNoWaitFor[typeform.GetFormResult]
}

func (recordQuestions) GetStepType() string { return "RecordQuestions" }

func (recordQuestions) Execute(ctx dex.Context, result typeform.GetFormResult) (*dex.StepDecision, error) {
	submission, err := submissionAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	questions, unmatched := PairQuestionsWithAnswers(result.Value, submission)
	recorded := RecordedQuestions{Branch: result.Branch, Questions: questions, UnmatchedAnswers: unmatched}
	if err := questionsAttribute.Set(ctx, recorded); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(recorded), nil
}

// dex:group group-id:typeform group-label:"Typeform"
// dex:explanation text:"Persist the notFound, providerRejected, invalidResponse, or defect outcome with its safe message and fail the Flow."
type recordReadFailure struct {
	dex.StepDefaultsNoWaitFor[typeform.GetFormResult]
}

func (recordReadFailure) GetStepType() string { return "RecordReadFailure" }

func (recordReadFailure) Execute(ctx dex.Context, result typeform.GetFormResult) (*dex.StepDecision, error) {
	recorded := RecordedQuestions{Branch: result.Branch}
	message := "reading the form selected " + string(result.Branch)
	if result.Failure != nil {
		recorded.FailureKind, recorded.FailureMessage = result.Failure.Kind, result.Failure.Message
		message += ": " + result.Failure.Message
	}
	if err := questionsAttribute.Set(ctx, recorded); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
