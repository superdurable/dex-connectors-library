// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package responserecorder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func submissionEvent(eventID string, answers []typeform.FormAnswer) sdkgo.TriggerEvent[typeform.FormResponseEvent] {
	submittedAt := time.Date(2026, time.September, 30, 11, 58, 59, 0, time.UTC)
	return sdkgo.TriggerEvent[typeform.FormResponseEvent]{ID: eventID, OccurredAt: submittedAt, Payload: typeform.FormResponseEvent{
		WebhookEventID: "LtWXD3crgy", FormID: "lT4Z3j", FormTitle: "Lead intake",
		Response: typeform.FormResponse{
			Token: "a3a12ec67a1365927098a606107fac15", SubmittedAt: submittedAt, Answers: answers,
			HiddenFields: map[string]string{"user_id": "abc123456"},
		},
	}}
}

func TestRedeliveriesResolveOneFlowAndCarryTheAnswers(t *testing.T) {
	answers := []typeform.FormAnswer{{FieldID: "SMEUb7VJz92Q", FieldRef: "email", FieldType: "email", Type: typeform.AnswerTypeEmail, Email: "ada@example.com"}}
	event := submissionEvent("lT4Z3j:a3a12ec67a1365927098a606107fac15", answers)
	redelivery := event
	redelivery.Payload.WebhookEventID = "SecondHook1"
	require.Equal(t, "typeform-lT4Z3j-a3a12ec67a1365927098a606107fac15", ResolveFlowID(event))
	require.Equal(t, ResolveFlowID(event), ResolveFlowID(redelivery), "a second webhook of the form maps to the same Flow")
	submission := MapToFlowInput(event)
	require.Equal(t, Submission{
		EventID: event.ID, FormID: "lT4Z3j", FormTitle: "Lead intake", ResponseToken: "a3a12ec67a1365927098a606107fac15",
		SubmittedAt: event.OccurredAt, Answers: answers, HiddenFields: map[string]string{"user_id": "abc123456"},
	}, submission)
	require.Equal(t, typeform.GetFormInput{FormID: "lT4Z3j"}, MapToGetFormInput(submission))
}

func TestAcceptSubmissionAdmitsOnlySubmissionsWithAnswers(t *testing.T) {
	require.True(t, AcceptSubmission(submissionEvent("a", []typeform.FormAnswer{{FieldID: "x", FieldType: "short_text", Type: "text", Text: "Ada"}})))
	require.False(t, AcceptSubmission(submissionEvent("b", nil)), "a form submitted with every question skipped is not recorded")
}

func TestPairQuestionsWithAnswersMatchesByFieldIDAndKeepsSkippedAndRemovedQuestions(t *testing.T) {
	email := typeform.FormAnswer{FieldID: "SMEUb7VJz92Q", FieldRef: "generated-ref-1", FieldType: "email", Type: typeform.AnswerTypeEmail, Email: "ada@example.com"}
	removed := typeform.FormAnswer{FieldID: "REMOVEDfield", FieldType: "short_text", Type: typeform.AnswerTypeText, Text: "old question"}
	form := typeform.Form{ID: "lT4Z3j", Fields: []typeform.FormField{
		{ID: "SMEUb7VJz92Q", Ref: "generated-ref-2", Title: "Your email?", Type: "email"},
		{ID: "RUqkXSeXBXSd", Ref: "consent", Title: "May we follow up?", Type: "yes_no"},
	}}
	questions, unmatched := PairQuestionsWithAnswers(form, Submission{Answers: []typeform.FormAnswer{email, removed}})
	require.Equal(t, []RecordedQuestion{{Field: form.Fields[0], Answer: &email}, {Field: form.Fields[1]}}, questions,
		"a changed generated ref still matches by field ID, and a skipped question has no answer")
	require.Equal(t, []typeform.FormAnswer{removed}, unmatched)
}

func TestFlowDeclaresItsResponseSubmittedBindingWithTheFormPicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, ResponseSubmittedTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Equal(t, typeform.ResponseSubmittedTriggerDefinition, bindings[0].Definition)
	require.NotNil(t, bindings[0].ConfigurationUI)
	units := bindings[0].ConfigurationUI.Units
	require.Len(t, units, 1)
	require.Equal(t, typeform.UIUnitFormPicker, units[0].UnitID)
	require.False(t, units[0].Required, "blank records every form")
	require.Contains(t, units[0].Description, "Leave it empty")
	require.Contains(t, units[0].Description, "stores the form ID")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: typeform.UIFormPickerPortFormID, JSONPointer: "/formId"}}, units[0].Bindings)
	require.Equal(t, FlowType, (&Flow{}).GetFlowType())
}
