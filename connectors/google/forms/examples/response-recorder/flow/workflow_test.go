// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package responserecorder

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	nine        = time.Date(2026, time.September, 30, 9, 0, 0, 0, time.UTC)
	nineFifteen = time.Date(2026, time.September, 30, 9, 15, 0, 0, time.UTC)
)

func TestNextCursorKeepsEveryResponseAtTheLatestInstant(t *testing.T) {
	require.Nil(t, NextCursor(nil, nil))
	require.Equal(t, &ResponseCursor{LastSubmittedAt: nineFifteen, ResponseIDs: []string{"r2", "r3"}}, NextCursor(nil, []RecordedResponse{
		{ResponseID: "r1", LastSubmittedAt: nine}, {ResponseID: "r2", LastSubmittedAt: nineFifteen}, {ResponseID: "r3", LastSubmittedAt: nineFifteen},
	}))

	previous := &ResponseCursor{LastSubmittedAt: nineFifteen, ResponseIDs: []string{"r2"}}
	require.Equal(t, previous, NextCursor(previous, nil))
	require.Equal(t, &ResponseCursor{LastSubmittedAt: nineFifteen, ResponseIDs: []string{"r2", "r4"}}, NextCursor(previous, []RecordedResponse{
		{ResponseID: "r4", LastSubmittedAt: nineFifteen}, {ResponseID: "r2", LastSubmittedAt: nineFifteen},
	}))
	require.Equal(t, []string{"r2"}, previous.ResponseIDs, "NextCursor must not change the previous cursor")
	require.Equal(t, &ResponseCursor{LastSubmittedAt: nineFifteen.Add(time.Nanosecond), ResponseIDs: []string{"r5"}}, NextCursor(previous, []RecordedResponse{
		{ResponseID: "r5", LastSubmittedAt: nineFifteen.Add(time.Nanosecond)},
	}))
}

func TestIsRecordedBeforeSkipsOnlyTheCursorInstantAndOlderResponses(t *testing.T) {
	cursor := &ResponseCursor{LastSubmittedAt: nineFifteen, ResponseIDs: []string{"r2"}}
	require.False(t, isRecordedBefore(nil, forms.FormResponse{ResponseID: "r2", LastSubmittedAt: nineFifteen}))
	require.True(t, isRecordedBefore(cursor, forms.FormResponse{ResponseID: "r2", LastSubmittedAt: nineFifteen}))
	require.False(t, isRecordedBefore(cursor, forms.FormResponse{ResponseID: "r3", LastSubmittedAt: nineFifteen}))
	require.True(t, isRecordedBefore(cursor, forms.FormResponse{ResponseID: "r0", LastSubmittedAt: nine}))
	require.False(t, isRecordedBefore(cursor, forms.FormResponse{ResponseID: "r2", LastSubmittedAt: nineFifteen.Add(time.Second)}), "an edited response is new again")
}

func TestRecordFormResponseLabelsAnswersInFormOrder(t *testing.T) {
	form := RecordedForm{FormID: "f1", Questions: []RecordedQuestion{
		{QuestionID: "q_name", Title: "Company name"}, {QuestionID: "q_contract", Title: "Contract"}, {QuestionID: "q_skipped", Title: "Skipped"},
	}}
	recorded := RecordFormResponse(form, forms.FormResponse{
		ResponseID: "r1", CreatedAt: nine, LastSubmittedAt: nineFifteen, RespondentEmail: "buyer@example.com",
		Answers: []forms.FormAnswer{
			{QuestionID: "q_contract", Files: []forms.FormAnswerFile{{FileID: "file_msa", FileName: "MSA.pdf"}}},
			{QuestionID: "q_name", Values: []string{"Acme"}},
			{QuestionID: "q_removed", Values: []string{"old"}},
		},
	})
	require.Equal(t, RecordedResponse{
		ResponseID: "r1", CreatedAt: nine, LastSubmittedAt: nineFifteen, IsEdited: true, RespondentEmail: "buyer@example.com",
		Answers: []RecordedAnswer{
			{QuestionID: "q_name", Question: "Company name", Values: []string{"Acme"}},
			{QuestionID: "q_contract", Question: "Contract", FileNames: []string{"MSA.pdf"}},
			{QuestionID: "q_removed", Values: []string{"old"}},
		},
	}, recorded)
	require.False(t, RecordFormResponse(form, forms.FormResponse{ResponseID: "r2", CreatedAt: nine, LastSubmittedAt: nine}).IsEdited)
}

func TestValidateInputRejectsUnservableRequests(t *testing.T) {
	picked := FormConfiguration{FormID: "f1"}
	require.Empty(t, validateInput(Input{}, picked))
	require.Empty(t, validateInput(Input{Cursor: &ResponseCursor{LastSubmittedAt: nine}}, picked))
	require.Empty(t, validateInput(Input{ResponseID: "r1"}, picked))
	tooManyIDs := make([]string, maxCursorResponseIDs+1)
	for name, test := range map[string]struct {
		input Input
		form  FormConfiguration
	}{
		"no picked form":         {input: Input{}, form: FormConfiguration{}},
		"cursor and response ID": {input: Input{Cursor: &ResponseCursor{LastSubmittedAt: nine}, ResponseID: "r1"}, form: picked},
		"cursor without time":    {input: Input{Cursor: &ResponseCursor{}}, form: picked},
		"oversized cursor":       {input: Input{Cursor: &ResponseCursor{LastSubmittedAt: nine, ResponseIDs: tooManyIDs}}, form: picked},
		"padded response ID":     {input: Input{ResponseID: " r1"}, form: picked},
	} {
		require.NotEmpty(t, validateInput(test.input, test.form), name)
	}
}

func TestConnectorMappersUseThePickedFormAndBoundedPages(t *testing.T) {
	flow := NewFlow(forms.Connection{}, sdkgo.ConnectorLoadedConfiguration[FormConfiguration]{Value: FormConfiguration{FormID: "f1"}})
	require.Equal(t, forms.GetFormInput{FormID: "f1"}, flow.MapToGetFormInput(Input{}))
	require.Equal(t, forms.ListResponsesInput{FormID: "f1", SubmittedAtOrAfter: nine, PageSize: ResponsePageSize, PageToken: "p2"},
		MapToListResponsesInput(ResponsePageRequest{FormID: "f1", SubmittedAtOrAfter: nine, PageToken: "p2"}))
	require.Equal(t, forms.GetResponseInput{FormID: "f1", ResponseID: "r1"}, MapToGetResponseInput(forms.GetResponseInput{FormID: "f1", ResponseID: "r1"}))
}

func TestFormConfigurationRefMatchesItsStep(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: forms.ConnectorID, ConnectionName: ConnectionName, OperationID: "getForm", FlowType: FlowType, StepType: readFormStepType,
	}, FormConfigurationRef())
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(forms.Connection{}, sdkgo.ConnectorLoadedConfiguration[FormConfiguration]{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordRecorderRequest{}))
	require.Equal(t, indexQuestionsStepType, dex.GetFinalStepType[forms.GetFormResult](indexFormQuestions{}))
	require.Equal(t, recordPageStepType, dex.GetFinalStepType[forms.ListResponsesResult](recordResponsePage{}))
	require.Equal(t, recordResponseStepType, dex.GetFinalStepType[forms.GetResponseResult](recordNamedResponse{}))
	require.Equal(t, reportNotFoundStepType, dex.GetFinalStepType[forms.GetResponseResult](reportResponseNotFound{}))
	wait, err := recordRecorderRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := forms.New(forms.Config{}, sdkgo.StaticCredentialProvider[forms.Credentials]{})
	require.NoError(t, err)
	connection, err := forms.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	unset := sdkgo.ConnectorLoadedConfiguration[FormConfiguration]{}
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, unset)})
	require.NoError(t, err)

	otherConnection, err := forms.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, unset)}) })
}
