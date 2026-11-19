// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestPlanQueriesExcludePricingInternals(t *testing.T) {
	plan := `{"id":"e28zov4fw0v2","code":"standard","name":"Standard","currencies":[{"currency":"USD","unit_amount":10.5}],"private":"ignored"}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/plans" {
			require.Equal(t, "2", request.URL.Query().Get("limit"))
			_, err := writer.Write([]byte(`{"object":"list","has_more":false,"data":[` + plan + `]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "/api/plans/e28zov4fw0v2", request.URL.Path)
		_, err := writer.Write([]byte(plan))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListPlans(), testConnection, ListPlansInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, ListPlansBranchListed, listed.Branch)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetPlan(), testConnection, GetPlanInput{PlanID: "e28zov4fw0v2"})
	require.NoError(t, err)
	require.Equal(t, GetPlanBranchFound, found.Branch)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), "unit_amount")
}

func TestCreatePlanUsesCallDerivedCodeAndFixedPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/plans", request.URL.Path)
		var body struct {
			Code         string `json:"code"`
			Name         string `json:"name"`
			PricingModel string `json:"pricing_model"`
			Currencies   []struct {
				Currency   string  `json:"currency"`
				UnitAmount float64 `json:"unit_amount"`
			} `json:"currencies"`
			IntervalUnit   string `json:"interval_unit"`
			IntervalLength int    `json:"interval_length"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Regexp(t, `^standard_[0-9a-f]{32}$`, body.Code)
		require.Equal(t, "fixed", body.PricingModel)
		require.Equal(t, "USD", body.Currencies[0].Currency)
		require.Equal(t, 10.5, body.Currencies[0].UnitAmount)
		_, err := writer.Write([]byte(`{"id":"e28zov4fw0v2","code":"` + body.Code + `","name":"Standard"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := CreatePlanInput{CodePrefix: "standard", Name: "Standard", CurrencyCode: "USD", UnitAmount: 10.5, IntervalUnit: "months", IntervalLength: 1}
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("recurly-plan", "create"), newTestClient(t, server.URL+"/api", 0).CreatePlan(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreatePlanBranchCreated, result.Branch)
}
