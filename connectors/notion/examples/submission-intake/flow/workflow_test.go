// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package submissionintake

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validInput() Input {
	return Input{DatabaseTitle: " Form submissions ", SubmissionID: " sub-42 ", Name: " Ada Lovelace ", Email: "ada@example.com", Message: "Hello."}
}

func TestValidateSubmissionTrimsAndRejectsUnusableInput(t *testing.T) {
	submission, err := validateSubmission(validInput())
	require.NoError(t, err)
	require.Equal(t, Input{DatabaseTitle: "Form submissions", SubmissionID: "sub-42", Name: "Ada Lovelace", Email: "ada@example.com", Message: "Hello."}, submission)
	for name, change := range map[string]func(*Input){
		"blank database title":     func(input *Input) { input.DatabaseTitle = " " },
		"multi-line submission ID": func(input *Input) { input.SubmissionID = "sub\n42" },
		"blank name":               func(input *Input) { input.Name = "" },
		"named email":              func(input *Input) { input.Email = "Ada <ada@example.com>" },
		"long message":             func(input *Input) { input.Message = strings.Repeat("a", maximumMessageRunes+1) },
	} {
		input := validInput()
		change(&input)
		_, err := validateSubmission(input)
		require.Error(t, err, name)
	}
}

func TestMappersBuildTheSubmissionKeyedRequests(t *testing.T) {
	record := SubmissionRecord{
		Submission:   Input{DatabaseTitle: "Form submissions", SubmissionID: "sub-42", Name: "Ada", Email: "ada@example.com", Message: "Hello."},
		SubmittedAt:  "2026-09-30T16:00:00Z",
		DataSourceID: "2a4d6f80-1b3c-4d5e-8f70-112233445566", PageID: "1f3c9a2e-5b7d-4e8a-9c01-23456789abcd",
	}
	require.Equal(t, notion.SearchInput{Query: "Form submissions", ObjectType: notion.SearchObjectDataSource, PageSize: searchPageSize},
		MapToFindSubmissionsDataSourceInput(record))
	query := MapToFindSubmissionRowsInput(record)
	require.Equal(t, record.DataSourceID, query.DataSourceID)
	require.Equal(t, &notion.QueryFilter{Property: SubmissionIDProperty, Type: notion.PropertyTypeRichText, Condition: notion.FilterEquals, Text: "sub-42"}, query.Filter)
	require.Equal(t, 2, query.PageSize, "two rows are enough to see a duplicate")

	create := MapToCreateSubmissionRowInput(record)
	require.Equal(t, map[string]notion.PropertyValue{
		notion.TitlePropertyID: notion.TitleValue("Ada"), EmailProperty: notion.EmailValue("ada@example.com"),
		SubmittedAtProperty: notion.DateValue("2026-09-30T16:00:00Z"), SubmissionIDProperty: notion.RichTextValue("sub-42"),
	}, create.Properties)
	require.Equal(t, "Hello.", create.BodyText)
	update := MapToUpdateSubmissionRowInput(record)
	require.Equal(t, record.PageID, update.PageID)
	require.NotContains(t, update.Properties, SubmissionIDProperty, "the business key never changes")
	require.Equal(t, notion.GetPageInput{PageID: record.PageID, MaxTextCharacters: readBackMaximumTextCharacters}, MapToReadBackSubmissionInput(record))
}

func TestExactTitleMatchIgnoresCaseTrashAndPartialTitles(t *testing.T) {
	matches := []notion.SearchMatch{
		{ObjectType: notion.SearchObjectDataSource, ID: "a", Title: "form submissions"},
		{ObjectType: notion.SearchObjectDataSource, ID: "b", Title: "Form submissions (2025)"},
		{ObjectType: notion.SearchObjectDataSource, ID: "c", Title: "Form submissions", IsInTrash: true},
		{ObjectType: notion.SearchObjectPage, ID: "d", Title: "Form submissions"},
	}
	require.Equal(t, []string{"a"}, exactTitleMatchIDs(matches, "Form submissions"))
}

func TestTheFlowRegistersEveryStepAndTheAttribute(t *testing.T) {
	client, err := notion.New(notion.Config{Endpoint: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[notion.Credentials]{})
	require.NoError(t, err)
	connection, err := notion.NewConnection(client, sdkgo.ConnectionRef{Provider: "notion", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection)
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Equal(t, []dex.AttributeDef{submissionAttribute}, flow.GetPersistenceSchema().Attributes)
}
