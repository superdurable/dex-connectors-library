//go:build integration && live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskcrud

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/doist/todoist"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestLiveTaskCRUD verifies partial updates through the same Flow on three new disposable tasks.
func TestLiveTaskCRUD(t *testing.T) {
	require.Equal(t, "true", os.Getenv("TODOIST_CONNECTOR_ALLOW_LOCAL_TEST"), "explicit local live-test opt-in required")
	contents, err := os.ReadFile(os.Getenv("DEX_CONNECTOR_CONFIG_FILE"))
	require.NoError(t, err)
	var localFile struct {
		Connections []struct {
			ConnectorID    string          `json:"connectorId"`
			ConnectionName string          `json:"connectionName"`
			Credentials    json.RawMessage `json:"credentials"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &localFile))
	var credential todoist.Credentials
	for _, record := range localFile.Connections {
		if record.ConnectorID == todoist.ConnectorID && record.ConnectionName == ConnectionName {
			var fields struct {
				APIToken string `json:"api_token"`
			}
			require.NoError(t, projectconfig.DecodeCredentials(record.Credentials, &fields))
			credential = todoist.Credentials{APIToken: sdkgo.NewSecretString(fields.APIToken)}
		}
	}
	require.NoError(t, credential.Validate())
	reference := sdkgo.ConnectionRef{Provider: "todoist", Name: ConnectionName}
	credentials := sdkgo.StaticCredentialProvider[todoist.Credentials]{reference: credential}
	providerClient, err := todoist.New(todoist.Config{}, credentials)
	require.NoError(t, err)
	connection, err := todoist.NewConnection(providerClient, reference)
	require.NoError(t, err)

	flow, harness := newCRUDHarnessWithConnection(t, connection)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	description, emptyDescription, newContent := "Updated description", "", "Updated title"
	for _, input := range []Request{
		{Mode: "lifecycle", Content: "Dex Todoist patch description test", Description: "Original description", UpdateDescription: &description},
		{Mode: "lifecycle", Content: "Dex Todoist clear description test", Description: "Original description", UpdateDescription: &emptyDescription},
		{Mode: "lifecycle", Content: "Dex Todoist patch both test", Description: "Original description", UpdateContent: &newContent, UpdateDescription: &description},
	} {
		flowID := fmt.Sprintf("todoist-live-crud-%s-%d", input.Mode, time.Now().UnixNano())
		result := harness.runFlow(t, ctx, flow, flowID, input)
		require.Equal(t, dex.FlowCompleted, result.Status, "inspect created Attribute if a later lifecycle Step failed")
		var outcome Outcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		if input.Mode == "list" {
			require.Equal(t, "listed", outcome.Status)
			t.Logf("Flow %s: listed %d active tasks, next page=%t", flowID, len(outcome.Page.Tasks), outcome.Page.NextCursor != "")
		} else {
			require.Equal(t, "deletedAndVerified", outcome.Status)
			require.Equal(t, input.Content, outcome.Created.Content)
			expectedContent, expectedDescription := input.Content, input.Description
			if input.UpdateContent != nil {
				expectedContent = *input.UpdateContent
			}
			if input.UpdateDescription != nil {
				expectedDescription = *input.UpdateDescription
			}
			require.Equal(t, input.Description, outcome.Created.Description)
			require.Equal(t, expectedContent, outcome.Updated.Content)
			require.Equal(t, expectedDescription, outcome.Updated.Description)
			require.Equal(t, outcome.Created.ProjectID, outcome.Updated.ProjectID)
			require.Equal(t, "deleted", outcome.DeleteBranch)
			t.Logf("Flow %s: created, read, updated, listed, deleted, and confirmed not-found; taskId=%s", flowID, outcome.Created.ID)
		}
		var display map[string]any
		require.NoError(t, harness.client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
		encoded, err := json.Marshal(display)
		require.NoError(t, err)
		require.False(t, bytes.Contains(encoded, []byte(credential.APIToken.Reveal())), "secret reached Display data")
	}
}
