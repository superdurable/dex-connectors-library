// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	responserecorder "github.com/superdurable/dex-connectors-library/connectors/google/forms/examples/response-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestEnvironmentOr(t *testing.T) {
	t.Setenv("GOOGLE_FORMS_EXAMPLE_VALUE", "configured")
	require.Equal(t, "configured", environmentOr("GOOGLE_FORMS_EXAMPLE_VALUE", "fallback"))
	require.Equal(t, "fallback", environmentOr("GOOGLE_FORMS_EXAMPLE_MISSING", "fallback"))
}

func TestLoadFormConfigurationTreatsAMissingPickAsBlank(t *testing.T) {
	loaded, err := loadFormConfiguration(projectconfig.Configuration{}, responserecorder.FormConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, responserecorder.FormConfigurationRef(), loaded.Reference)
	require.Empty(t, loaded.Value.FormID)
}

func TestLoadFormConfigurationReadsTheSavedPick(t *testing.T) {
	configuration := projectconfig.Configuration{OperationConfigurations: []projectconfig.OperationConfiguration{{
		ConnectorID: "google-forms", ConnectionName: "google-forms-intake", OperationID: "getForm",
		FlowType: "GoogleFormsResponseRecorder", StepType: "ReadForm",
		Configuration: json.RawMessage(`{"formId":"1FAIpQLintake","formTitle":"Vendor intake"}`),
	}}}
	loaded, err := loadFormConfiguration(configuration, responserecorder.FormConfigurationRef())
	require.NoError(t, err)
	require.Equal(t, responserecorder.FormConfiguration{FormID: "1FAIpQLintake", FormTitle: "Vendor intake"}, loaded.Value)
}
