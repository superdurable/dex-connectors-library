//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package publishpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/confluencemock"
)

func TestPolicyPublicationUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := confluencemock.New(t, ConnectionName)
	flow, harness := newConfluenceIntegrationHarnessWithConnection(t, mock.Connection())
	input := Input{
		Title: "Remote work policy", SpaceKey: "OPS",
		Body:               "# Remote work\n\nStaff may work **remotely** two days a week.\n\n- Ask your manager\n- Log the days",
		PublicationComment: "Published for the **Q4** review.",
	}
	publication := runPublicationFlow(t, integrationContext(t), harness, flow, "mock-default", input)

	require.Equal(t, PhasePublished, publication.Phase)
	require.False(t, publication.IsExistingPageUpdated)
	require.Len(t, publication.RelatedPolicies, 1, "the Flow records the related page from its first search page")
	require.NotEqual(t, publication.RelatedPolicies[0].PageID, publication.PageID)
	searches := mock.SearchPages().Calls()
	require.Len(t, searches, 1, "the example reads only the first search page")
	require.Equal(t, confluence.SearchPagesInput{
		Filter: confluence.PageSearchFilter{SpaceKeys: []string{"OPS"}, TitlePhrase: "Remote work policy"}, PageSize: 10,
	}, searches[0].Input)
	creates := mock.CreatePage().Calls()
	require.Len(t, creates, 1)
	require.Equal(t, confluence.CreatePageInput{
		SpaceKey: "OPS", Title: "Remote work policy",
		Body:       "# Remote work\n\nStaff may work **remotely** two days a week.\n\n- Ask your manager\n- Log the days",
		BodyFormat: confluence.TextFormatMarkdown,
	}, creates[0].Input)
	require.Empty(t, mock.UpdatePage().Calls())
	reads := mock.GetPage().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, confluence.GetPageInput{
		PageID: "600002", BodyFormat: confluence.TextFormatMarkdown, MaxBodyCharacters: 4000,
	}, reads[0].Input)
	comments := mock.AddComment().Calls()
	require.Len(t, comments, 1)
	require.Equal(t, confluence.AddCommentInput{
		PageID: "600002", Body: "Published for the **Q4** review.", BodyFormat: confluence.TextFormatMarkdown,
	}, comments[0].Input)
}
