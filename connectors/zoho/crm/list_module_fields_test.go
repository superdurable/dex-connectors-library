// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func dealFieldsJSON() map[string]any {
	return map[string]any{"fields": []any{
		map[string]any{"api_name": "Deal_Name", "display_label": "Deal Name", "data_type": "text", "custom_field": false, "system_mandatory": true,
			"read_only": false, "length": 120, "unique": map[string]any{}, "pick_list_values": []any{}, "profiles": []any{map[string]any{"name": "SENTINEL"}}},
		map[string]any{"api_name": "Stage", "display_label": "Stage", "data_type": "picklist", "unique": map[string]any{}, "pick_list_values": []any{
			map[string]any{"display_value": "Qualification", "actual_value": "Qualification", "type": "used", "id": "1"},
			map[string]any{"display_value": "Closed Won", "actual_value": "Closed Won", "type": "used", "id": "2"},
			map[string]any{"display_value": "Retired Stage", "actual_value": "Retired Stage", "type": "unused", "id": "3"},
		}},
		map[string]any{"api_name": "Account_Name", "display_label": "Account Name", "data_type": "lookup",
			"lookup": map[string]any{"display_label": "Account Name", "api_name": "Account_Name", "module": map[string]any{"api_name": "Accounts", "id": "41"}}},
		map[string]any{"api_name": "External_Deal_ID", "display_label": "External Deal ID", "data_type": "text", "custom_field": true,
			"unique": map[string]any{"case_sensitive": false}, "length": 50},
	}}
}

func TestListModuleFieldsReportsTypesUniquenessLookupsAndPicklistValues(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, dealFieldsJSON())
	})
	result, err := sdkgo.RunQuery(newCRMDexContext("fields"), newCRMClient(t, provider.URL).ListModuleFields(), crmConnection, crm.ListModuleFieldsInput{Module: crm.ModuleDeals})
	require.NoError(t, err)
	require.Equal(t, crm.ListModuleFieldsBranchListed, result.Branch)
	require.Len(t, result.Value.Fields, 4)
	require.Equal(t, crm.ModuleField{APIName: "Deal_Name", DisplayLabel: "Deal Name", DataType: "text", IsSystemMandatory: true, Length: 120}, result.Value.Fields[0])
	stage, isFound := result.Value.FieldNamed(crm.FieldStage)
	require.True(t, isFound)
	require.True(t, stage.HasPicklistValue("Closed Won"))
	require.False(t, stage.HasPicklistValue("Retired Stage"), "an unused option is not offered")
	require.False(t, stage.HasPicklistValue("closed won"), "picklist values compare exactly")
	account, _ := result.Value.FieldNamed(crm.FieldAccountName)
	require.Equal(t, crm.ModuleAccounts, account.LookupModule)
	external, _ := result.Value.FieldNamed("External_Deal_ID")
	require.True(t, external.IsUnique)
	require.True(t, external.IsCustom)
	requireNoSentinel(t, result)
	request := provider.request(0)
	require.Equal(t, "/crm/v8/settings/fields", request.path)
	require.Equal(t, url.Values{"module": {"Deals"}}, request.query)
}

func TestListModuleFieldsRejectsAMalformedListAndAMissingScope(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{"fields":[{"api_name":"Stage; drop","data_type":"picklist"}]}`)
			return
		}
		writeJSON(t, response, http.StatusUnauthorized, `{"code":"OAUTH_SCOPE_MISMATCH","details":{},"message":"SENTINEL","status":"error"}`)
	})
	client := newCRMClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCRMDexContext("fields"), client.ListModuleFields(), crmConnection, crm.ListModuleFieldsInput{Module: crm.ModuleDeals})
	require.NoError(t, err)
	require.Equal(t, crm.ListModuleFieldsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(newCRMDexContext("fields"), client.ListModuleFields(), crmConnection, crm.ListModuleFieldsInput{Module: crm.ModuleDeals})
	require.NoError(t, err)
	require.Equal(t, crm.ListModuleFieldsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "ZohoCRM.settings.fields.READ")
	requireNoSentinel(t, result)
}
