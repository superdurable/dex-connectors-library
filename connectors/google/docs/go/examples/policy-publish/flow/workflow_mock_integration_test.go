//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package policypublish

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/go/docsmock"
)

func TestPolicyPublishUsesMockedGoogleDocsWithRealDex(t *testing.T) {
	mock := docsmock.New(t, ConnectionName)
	mock.GetDocumentText().Respond(
		docsmock.GetDocumentTextDefault(),
		docsmock.GetDocumentTextRead(docs.GetDocumentTextOutput{
			DocumentID: "policyDraft1", Title: "Acme Refund Policy", RevisionID: "rev-draft-1",
			Format: docs.TextFormatPlainText, Text: "Refund Policy\nEffective {{effectiveDate}} for {{companyName}} customers.\n",
		}, nil),
		docsmock.GetDocumentTextRead(docs.GetDocumentTextOutput{
			DocumentID: "policyDraft1", Title: "Acme Refund Policy", RevisionID: "rev-draft-3",
			Format: docs.TextFormatMarkdown, Text: "# Refund Policy\n\nEffective 2026-10-01 for Acme customers.\nPublished by Dex.\n",
		}, nil),
	).Strict()
	mock.CreateDocument().Strict().Respond(docsmock.CreateDocumentDefault())
	mock.ReplaceDocumentText().Strict().Respond(docsmock.ReplaceDocumentTextDefault())
	mock.AppendText().Strict().Respond(docsmock.AppendTextDefault())
	harness := newPublishHarnessForConnection(t, mock.Connection())
	placeholders := []docs.PlaceholderReplacement{
		{Placeholder: "{{effectiveDate}}", Text: "2026-10-01"},
		{Placeholder: "{{companyName}}", Text: "Acme"},
	}
	outcome := harness.runPublish(t, "mock", Input{Title: "Acme Refund Policy", Placeholders: placeholders})
	require.Equal(t, StatusPublished, outcome.Status)
	require.Equal(t, "rev-template-1", outcome.TemplateRevisionID)
	require.Equal(t, "policyDraft1", outcome.Document.DocumentID)
	require.Empty(t, outcome.RemainingPlaceholders)
	require.Equal(t, "rev-draft-3", outcome.PublishedRevisionID)

	reads := mock.GetDocumentText().Calls()
	require.Len(t, reads, 3)
	require.Equal(t, docs.GetDocumentTextInput{DocumentID: templateDocumentID, Format: docs.TextFormatMarkdown}, reads[0].Input)
	require.Equal(t, docs.GetDocumentTextInput{DocumentID: "policyDraft1", Format: docs.TextFormatPlainText}, reads[1].Input)
	require.Equal(t, docs.GetDocumentTextInput{DocumentID: "policyDraft1", Format: docs.TextFormatMarkdown}, reads[2].Input)
	creates := mock.CreateDocument().Calls()
	require.Len(t, creates, 1)
	require.Equal(t, docs.CreateDocumentInput{
		Title: "Acme Refund Policy", ParentFolderID: destinationFolderID,
		InitialText:       "# Refund Policy\n\nEffective {{effectiveDate}} for {{companyName}} customers.\n",
		InitialTextFormat: docs.TextFormatMarkdown,
	}, creates[0].Input)
	fills := mock.ReplaceDocumentText().Calls()
	require.Len(t, fills, 1)
	require.Equal(t, docs.ReplaceDocumentTextInput{
		DocumentID: "policyDraft1", RequiredRevisionID: "rev-draft-1",
		Target: docs.ReplaceTargetPlaceholders, Placeholders: placeholders,
	}, fills[0].Input)
	stamps := mock.AppendText().Calls()
	require.Len(t, stamps, 1)
	require.Equal(t, "policyDraft1", stamps[0].Input.DocumentID)
	require.Equal(t, "rev-draft-2", stamps[0].Input.RequiredRevisionID)
	require.Regexp(t, `^Published by Dex on \d{4}-\d{2}-\d{2} from template revision rev-template-1\.$`, stamps[0].Input.Text)
}
