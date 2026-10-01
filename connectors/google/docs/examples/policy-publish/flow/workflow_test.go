// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package policypublish

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func newTestFlow(templateDocumentID string, destinationFolderID string) *Flow {
	return NewFlow(docs.Connection{},
		sdkgo.ConnectorLoadedConfiguration[TemplateConfiguration]{Reference: TemplateConfigurationRef(), Value: TemplateConfiguration{DocumentID: templateDocumentID}},
		sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{Reference: DestinationFolderConfigurationRef(), Value: FolderConfiguration{FolderID: destinationFolderID}},
	)
}

func TestMappersReadTheSavedTemplateAndCreateInTheSavedFolder(t *testing.T) {
	flow := newTestFlow("templateDoc", "fld_policies")
	require.Equal(t, docs.GetDocumentTextInput{DocumentID: "templateDoc", Format: docs.TextFormatMarkdown}, flow.MapToReadTemplateInput(Input{}))
	require.Equal(t, docs.CreateDocumentInput{
		Title: "Acme Refund Policy", ParentFolderID: "fld_policies", InitialText: "# Policy", InitialTextFormat: docs.TextFormatMarkdown,
	}, flow.MapToCreateDocumentInput(DraftRequest{Title: "Acme Refund Policy", TemplateMarkdown: "# Policy"}))
	require.Empty(t, newTestFlow("templateDoc", "").MapToCreateDocumentInput(DraftRequest{Title: "a"}).ParentFolderID)
}

func TestMappersCarryTheRevisionFromStepToStep(t *testing.T) {
	placeholders := []docs.PlaceholderReplacement{{Placeholder: "{{effectiveDate}}", Text: "2026-10-01"}}
	require.Equal(t, docs.GetDocumentTextInput{DocumentID: "draftDoc", Format: docs.TextFormatPlainText},
		MapToReadDraftInput(docs.CreateDocumentResult{Value: docs.CreateDocumentOutput{Document: docs.CreatedDocument{DocumentID: "draftDoc"}}}))
	require.Equal(t, docs.ReplaceDocumentTextInput{
		DocumentID: "draftDoc", RequiredRevisionID: "rev-1", Target: docs.ReplaceTargetPlaceholders, Placeholders: placeholders,
	}, MapToFillPlaceholdersInput(FillRequest{DocumentID: "draftDoc", RevisionID: "rev-1", Placeholders: placeholders}))
	require.Equal(t, docs.AppendTextInput{DocumentID: "draftDoc", RequiredRevisionID: "rev-2", Text: "stamp"},
		MapToAppendStampInput(StampRequest{DocumentID: "draftDoc", RevisionID: "rev-2", Text: "stamp"}))
	require.Equal(t, docs.GetDocumentTextInput{DocumentID: "draftDoc", Format: docs.TextFormatMarkdown},
		MapToReadBackInput(docs.AppendTextResult{Value: docs.AppendTextOutput{DocumentID: "draftDoc"}}))
}

func TestPublicationHelpers(t *testing.T) {
	require.Equal(t, "Published by Dex on 2026-10-01 from template revision rev-9.", PublicationStampText("2026-10-01", "rev-9"))
	require.Equal(t, []string{"{{b}}"}, RemainingPlaceholders("a {{b}}", []docs.PlaceholderReplacement{{Placeholder: "{{a}}"}, {Placeholder: "{{b}}"}}))
}

func TestConfigurationRefsMatchTheirSteps(t *testing.T) {
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: docs.ConnectorID, ConnectionName: ConnectionName, OperationID: "getDocumentText", FlowType: FlowType, StepType: readTemplateStepType,
	}, TemplateConfigurationRef())
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: docs.ConnectorID, ConnectionName: ConnectionName, OperationID: "createDocument", FlowType: FlowType, StepType: createDocumentStepType,
	}, DestinationFolderConfigurationRef())
}

func TestStepIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, FlowType, dex.GetFinalFlowType(newTestFlow("", "")))
	require.Equal(t, recordRequestStepType, dex.GetFinalStepType[Input](recordPublishRequest{}))
	require.Equal(t, prepareDraftStepType, dex.GetFinalStepType[docs.GetDocumentTextResult](preparePolicyDraft{}))
	require.Equal(t, prepareFillStepType, dex.GetFinalStepType[docs.GetDocumentTextResult](preparePlaceholderFill{}))
	require.Equal(t, prepareStampStepType, dex.GetFinalStepType[docs.ReplaceDocumentTextResult](preparePublicationStamp{}))
	require.Equal(t, completeStepType, dex.GetFinalStepType[docs.GetDocumentTextResult](completePublication{}))
	require.Equal(t, reportUncertainStepType, dex.GetFinalStepType[docs.CreateDocumentResult](reportCreationUncertain{}))
	require.Equal(t, reportMissingStepType, dex.GetFinalStepType[docs.ReplaceDocumentTextResult](reportPlaceholderMissing{}))
	wait, err := recordPublishRequest{}.WaitFor(nil, Input{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	client, err := docs.New(docs.Config{}, sdkgo.StaticCredentialProvider[docs.Credentials]{})
	require.NoError(t, err)
	connection, err := docs.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName})
	require.NoError(t, err)
	template := sdkgo.ConnectorLoadedConfiguration[TemplateConfiguration]{}
	folder := sdkgo.ConnectorLoadedConfiguration[FolderConfiguration]{}
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection, template, folder)})
	require.NoError(t, err)

	otherConnection, err := docs.NewConnection(client, sdkgo.ConnectionRef{Provider: "google", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection, template, folder)}) })
}
