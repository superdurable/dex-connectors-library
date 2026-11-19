// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var testCreateJobInput = CreateJobInput{Name: "Dex example job", NotebookPath: "/Shared/dex-example", ExistingClusterID: "1234-567890-abcd"}

func TestCreateJobSendsNotebookSettingsAndReturnsProviderID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/2.2/jobs/create", request.URL.Path)
		require.Equal(t, "Bearer sample-databricks-api-token", request.Header.Get("Authorization"))
		var settings createJobSettings
		require.NoError(t, json.NewDecoder(request.Body).Decode(&settings))
		require.Equal(t, testCreateJobInput.Name, settings.Name)
		require.Equal(t, []createJobTask{{TaskKey: "main", ExistingClusterID: testCreateJobInput.ExistingClusterID, NotebookTask: createJobNotebook{NotebookPath: testCreateJobInput.NotebookPath}}}, settings.Tasks)
		_, err := response.Write([]byte(`{"job_id":123}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("databricks", "create-job"), client.CreateJob(), testConnection, testCreateJobInput)
	require.NoError(t, err)
	require.Equal(t, CreateJobBranchCreated, result.Branch)
	require.Equal(t, JobSummary{ID: "123", Name: "Dex example job"}, result.Value)
}

func TestCreateJobStopsAfterThreeUnknownSends(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		response.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	ctx := singledispatchtest.NewContext("databricks", "create-job-unknown")
	for attempt := 1; attempt <= 3; attempt++ {
		result, err := sdkgo.RunMutation(ctx, client.CreateJob(), testConnection, testCreateJobInput)
		if attempt < 3 {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.NotContains(t, retry.Failure.Message, "sample-databricks-api-token")
			ctx.AdvanceAttempt()
			continue
		}
		require.NoError(t, err)
		require.Equal(t, CreateJobBranchUncertain, result.Branch)
		require.NotContains(t, result.Failure.Message, "sample-databricks-api-token")
	}
	require.Equal(t, int32(3), requests.Load())
}
