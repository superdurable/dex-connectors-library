// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package llm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
)

type heartbeatRecorder struct {
	*testsupport.DexContext
	mu     sync.Mutex
	values []any
}

func (recorder *heartbeatRecorder) RecordHeartbeat(value any) error {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recorder.values = append(recorder.values, value)
	return nil
}

func (recorder *heartbeatRecorder) recorded() []any {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]any(nil), recorder.values...)
}

func TestAttemptRecordsNilHeartbeatsOnlyWhileInFlight(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		time.Sleep(250 * time.Millisecond)
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]string{"text": "done"})
	}))
	defer provider.Close()
	query, err := NewTextGenerationQuery(&TextGenerationQueryConfig{
		Definition: sdkgo.QueryDefinition{
			Operation:    sdkgo.OperationRef{ConnectorID: "heartbeat-lab", OperationID: TextGenerationOperationID},
			Branches:     TextGenerationBranchDefinitions(),
			StepDefaults: sdkgo.StepDefaults{ExecuteDurability: dex.StepDurabilitySync},
		},
		WireFormat: WireFormat{
			ProviderName: "heartbeat-lab", ModelIDRule: ModelIDRuleBody,
			CredentialHeader: CredentialHeader{Name: "Authorization", Prefix: "Bearer "},
			RulesForModel:    func(string) ModelRequestRules { return ModelRequestRules{} },
			EncodeRequest:    func(EncodeRequestInput) (EncodedRequest, error) { return EncodedRequest{Path: "/"}, nil },
			DecodeResponse: func(body []byte) (DecodedResponse, error) {
				return DecodedResponse{Parts: []ResponsePart{{Text: string(body)}}, ProviderFinishReason: "stop"}, nil
			},
			FinishReasons: map[string]FinishReason{"stop": FinishReasonStop},
		},
		BaseURL: provider.URL, ConnectionModel: "model", RequestTimeout: 10 * time.Second,
		ResolveCredential: func(sdkgo.Call) (sdkgo.SecretString, error) { return sdkgo.NewSecretString("key"), nil },
		MaxResponseBytes:  1 << 10,
	})
	require.NoError(t, err)
	query.heartbeatInterval = 20 * time.Millisecond
	recorder := &heartbeatRecorder{DexContext: testsupport.NewDexContext("flow", "step")}
	var _ dex.Context = recorder

	result, err := sdkgo.RunQuery(recorder, query, sdkgo.ConnectionRef{Provider: "heartbeat", Name: "lab"},
		TextGenerationRequest{Messages: []Message{{Role: MessageRoleUser, Text: "hi"}}})
	require.NoError(t, err)
	require.Equal(t, GeneratedBranchID, result.Branch, "failure: %+v", result.Failure)
	heartbeats := recorder.recorded()
	require.GreaterOrEqual(t, len(heartbeats), 5, "a 250 ms silence at a 20 ms interval records several heartbeats")
	for _, value := range heartbeats {
		require.Nil(t, value, "heartbeats clear the checkpoint and carry no payload")
	}
	// Elapsed time is the behavior: Dex rejects heartbeats after Execute returns.
	time.Sleep(100 * time.Millisecond)
	require.Len(t, recorder.recorded(), len(heartbeats), "no heartbeat is recorded after Invoke returns")
}
