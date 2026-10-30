// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package publishpolicy

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBuildPublicationRequestPrefersThePickedSpace(t *testing.T) {
	request, err := BuildPublicationRequest(SpaceSelection{SpaceID: "98306", SpaceKey: "OPS"}, Input{
		Title: "  Remote work policy ", Body: "Two days.", SpaceKey: "HR", ChangeSummary: " Annual review ", PublicationComment: " Published. ",
	})
	require.NoError(t, err)
	require.Equal(t, PublicationRequest{
		SpaceID: "98306", SpaceKey: "OPS", Title: "Remote work policy", Body: "Two days.", ChangeSummary: "Annual review", PublicationComment: "Published.",
	}, request)

	request, err = BuildPublicationRequest(SpaceSelection{}, Input{Title: "Remote work policy", Body: "Two days.", SpaceKey: " HR "})
	require.NoError(t, err)
	require.Equal(t, "HR", request.SpaceKey)
	require.Empty(t, request.SpaceID)

	for _, input := range []Input{
		{Title: "Remote work policy", Body: "Two days."},
		{SpaceKey: "OPS", Body: "Two days."},
		{SpaceKey: "OPS", Title: "one\ntwo", Body: "Two days."},
		{SpaceKey: "OPS", Title: "Remote work policy", Body: " "},
	} {
		_, err := BuildPublicationRequest(SpaceSelection{}, input)
		require.Error(t, err, "%+v", input)
	}
}

func TestMappersBuildTheConnectorInputs(t *testing.T) {
	request := PublicationRequest{SpaceKey: "OPS", ParentPageID: "65537", Title: "Remote work policy", Body: "Two days.", ChangeSummary: "Annual review"}
	cql, err := MapToSearchPagesInput(request).Filter.CQL()
	require.NoError(t, err)
	require.Equal(t, `type = page AND space in ("OPS") AND title ~ "\"Remote work policy\"" ORDER BY lastmodified DESC`, cql)
	require.Equal(t, confluence.CreatePageInput{
		SpaceKey: "OPS", ParentPageID: "65537", Title: "Remote work policy", Body: "Two days.", BodyFormat: confluence.TextFormatMarkdown,
	}, MapToCreatePageInput(request))
	request.SpaceID = "98306"
	require.Equal(t, "98306", MapToCreatePageInput(request).SpaceID)
	require.Empty(t, MapToCreatePageInput(request).SpaceKey)
	require.Equal(t, confluence.UpdatePageInput{
		PageID: "557057", Title: "Remote work policy", Body: "Two days.", BodyFormat: confluence.TextFormatMarkdown,
		NextVersionNumber: 4, VersionMessage: "Annual review",
	}, MapToUpdatePageInput(NewPageUpdateRequest(request, "557057", 3)))
	require.Equal(t, confluence.GetPageInput{PageID: "557057", BodyFormat: confluence.TextFormatMarkdown, MaxBodyCharacters: 4000},
		MapToGetPageInput(PageReference{PageID: "557057"}))
}

func TestFlowRegistersEveryStepAndRPC(t *testing.T) {
	client, err := confluence.New(confluence.Config{CloudID: "11223344-a1b2-4b33-8c44-def123456789"}, sdkgo.StaticCredentialProvider[confluence.Credentials]{})
	require.NoError(t, err)
	connection, err := confluence.NewConnection(client, sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection, SpaceSelection{SpaceKey: "OPS"})
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Len(t, flow.GetRPCs(), 4)
}
