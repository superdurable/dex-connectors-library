// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gemini_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/gemini/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = "/v1beta"
const conformanceFixturePathPrefix = ""

func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	client, err := gemini.New(gemini.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[gemini.Credentials]{
		geminiConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)},
	}, gemini.WithHTTPClient(transport))
	require.NoError(t, err)
	context := &geminiConformanceContext{DexContext: testsupport.NewDexContext("conformance", "gemini-generate")}
	previousAttempt := conformancePriorAttempt(t, scenario.PriorHeartbeat, func(value []byte) {
		context.previousHeartbeat = append(json.RawMessage(nil), value...)
	})
	return &conformanceMappedInvoker{t: t, scenario: scenario, before: func(attempt int) {
		if attempt > 1 {
			context.previousHeartbeat = context.recordedHeartbeat
			context.recordedHeartbeat = nil
		}
		context.AttemptNo = previousAttempt + int32(attempt)
	}, actions: map[string]conformanceOperationInvocation{
		"generateContent":       newConformanceQueryInvocation(t, scenario.Input, context, client.GenerateContent(), geminiConnection),
		"listFileSearchStores":  newConformanceQueryInvocation(t, scenario.Input, context, client.ListFileSearchStores(), geminiConnection),
		"getFileSearchStore":    newConformanceQueryInvocation(t, scenario.Input, context, client.GetFileSearchStore(), geminiConnection),
		"createFileSearchStore": newConformanceMutationInvocation(t, scenario.Input, context, client.CreateFileSearchStore(), geminiConnection, scenario.CallID),
		"deleteFileSearchStore": newConformanceMutationInvocation(t, scenario.Input, context, client.DeleteFileSearchStore(), geminiConnection, scenario.CallID),
	}}
}

type geminiConformanceContext struct {
	*testsupport.DexContext
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
}

func (context *geminiConformanceContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recordedHeartbeat = encoded
	return nil
}

func (context *geminiConformanceContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(context.previousHeartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, value)
}
