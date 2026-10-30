//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package companynews

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/newsapi/go"
	"github.com/superdurable/dex-connectors-library/connectors/newsapi/go/newsapimock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCompanyNewsUsesMockDefaultWithRealDex(t *testing.T) {
	mock := newsapimock.New(t, ConnectionName)
	flow, harness := newCompanyNewsIntegrationHarnessWithConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	input := CompanyNewsRequest{Company: " Meridian ", Days: 3, MaxArticles: 5}
	expectedFromBeforeRun := time.Now().UTC().AddDate(0, 0, -input.Days).Format(time.DateOnly)
	flowID := "newsapi-company-news-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	result := harness.runFlow(t, ctx, flow, flowID, input)
	expectedFromAfterRun := time.Now().UTC().AddDate(0, 0, -input.Days).Format(time.DateOnly)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome CompanyNewsOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, StatusFound, outcome.Status)
	require.Equal(t, "Meridian", outcome.Company)
	require.Equal(t, 2, outcome.TotalResults)
	require.Len(t, outcome.Articles, 2)
	require.Contains(t, []string{expectedFromBeforeRun, expectedFromAfterRun}, outcome.From)
	require.Contains(t, outcome.Articles[0].Title, outcome.Company)

	calls := mock.SearchArticles().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	expectedInput := newsapi.SearchArticlesInput{
		Query: "Meridian", SearchIn: []string{"title"}, From: expectedFromBeforeRun,
		Language: "en", SortBy: "publishedAt", PageSize: 5,
	}
	if expectedFromAfterRun != expectedFromBeforeRun && calls[0].Input.From == expectedFromAfterRun {
		expectedInput.From = expectedFromAfterRun
	}
	require.Equal(t, expectedInput, calls[0].Input)
	require.Contains(t, []string{expectedFromBeforeRun, expectedFromAfterRun}, calls[0].Input.From)
	require.Equal(t, newsapi.SearchArticlesBranchSearched, calls[0].Branch)
}
