// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package companynews

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/newsapi"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var preparationTime = time.Date(2026, 10, 5, 7, 0, 0, 0, time.FixedZone("PDT", -7*60*60))

func TestPrepareCompanyNewsRequestAppliesDefaultsAndFixesTheWindow(t *testing.T) {
	prepared, err := PrepareCompanyNewsRequest(CompanyNewsRequest{Company: "  Acme Corporation "}, preparationTime)
	require.NoError(t, err)
	require.Equal(t, CompanyNewsRequest{Company: "Acme Corporation", Days: 10, MaxArticles: 20, Language: "en", From: "2026-09-25"}, prepared)

	prepared, err = PrepareCompanyNewsRequest(CompanyNewsRequest{Company: "Acme", Days: 1, MaxArticles: 100, Language: "de"}, preparationTime)
	require.NoError(t, err)
	require.Equal(t, "2026-10-04", prepared.From, "the window is computed in UTC")
	require.Equal(t, "de", prepared.Language)
}

func TestPrepareCompanyNewsRequestRejectsInvalidInput(t *testing.T) {
	for name, request := range map[string]CompanyNewsRequest{
		"blank company":  {Company: "  "},
		"long company":   {Company: string(make([]rune, 101))},
		"negative days":  {Company: "Acme", Days: -1},
		"too many days":  {Company: "Acme", Days: 31},
		"page too large": {Company: "Acme", MaxArticles: 101},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := PrepareCompanyNewsRequest(request, preparationTime)
			require.Error(t, err)
		})
	}
}

func TestMapToSearchArticlesInputSearchesTitlesNewestFirst(t *testing.T) {
	input := MapToSearchArticlesInput(CompanyNewsRequest{Company: "AT&T", Days: 10, MaxArticles: 20, Language: "en", From: "2026-09-25"})
	require.Equal(t, newsapi.SearchArticlesInput{
		Query: "AT&T", SearchIn: []string{"title"}, From: "2026-09-25", Language: "en", SortBy: "publishedAt", PageSize: 20,
	}, input)
}

// TestStartFlowIdentitiesMatchTheFlowDefinition keeps registered types equal to the dexcli visualize names that Start Flow sends.
func TestStartFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	require.Equal(t, "NewsAPICompanyNews", dex.GetFinalFlowType(NewFlow(newsapi.Connection{})))
	require.Equal(t, "RecordCompanyNewsRequest", dex.GetFinalStepType[CompanyNewsRequest](recordCompanyNewsRequest{}))
	require.Equal(t, "CompanyArticlesFound", dex.GetFinalStepType[newsapi.SearchArticlesResult](companyArticlesFound{}))
	require.Equal(t, "CompanyNewsRejected", dex.GetFinalStepType[newsapi.SearchArticlesResult](companyNewsRejected{}))
	wait, err := recordCompanyNewsRequest{}.WaitFor(nil, CompanyNewsRequest{Company: "Acme"})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
}

func TestFlowRegistersOnlyWithTheStaticConnectionName(t *testing.T) {
	reference := sdkgo.ConnectionRef{Provider: "newsapi", Name: ConnectionName}
	client, err := newsapi.New(newsapi.Config{}, sdkgo.StaticCredentialProvider[newsapi.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString("0123456789abcdef0123456789abcdef")},
	})
	require.NoError(t, err)
	connection, err := newsapi.NewConnection(client, reference)
	require.NoError(t, err)
	_, err = dex.NewRegistry([]dex.Flow{NewFlow(connection)})
	require.NoError(t, err)

	otherConnection, err := newsapi.NewConnection(client, sdkgo.ConnectionRef{Provider: "newsapi", Name: "another-connection"})
	require.NoError(t, err)
	require.Panics(t, func() { _, _ = dex.NewRegistry([]dex.Flow{NewFlow(otherConnection)}) },
		"the static ConnectionName must match the runtime connection")
}
