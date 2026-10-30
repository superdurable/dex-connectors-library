// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo

import (
	"testing"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/internal/conformancefixture"
)

func TestConformanceFixtures(t *testing.T) {
	callID := deriveCallID("flow-42", "step-execution-7", OperationRef{ConnectorID: "sample", OperationID: "createItem"})
	if err := callID.Validate(); err != nil {
		t.Fatal(err)
	}
	if string(callID) != conformancefixture.ExampleCallID {
		t.Fatalf("derived call ID changed: %s", callID)
	}
	observedAt := time.Date(2026, 10, 29, 12, 30, 45, 123000000, time.UTC)
	fullReceipt := Receipt{
		CallID: callID, IdempotencyKey: IdempotencyKey("sample/" + callID), Provider: "sample",
		ProviderObjectID: "object-42", ProviderRequestID: "request-7", ObservedAt: observedAt,
		Metadata: map[string]string{"region": "west"},
	}
	minimalReceipt := Receipt{CallID: callID, Provider: "sample", ObservedAt: observedAt}
	failed := Failure{Kind: FailureNotFound, Provider: "sample", Operation: "getItem", Message: "item was not found"}
	unknown := Failure{Kind: FailureTransport, Provider: "sample", Operation: "createItem", Message: "write outcome cannot be confirmed"}
	conformancefixture.Check(t, "results.json", map[string]any{
		"queryHappy":              QueryResult[string]{Branch: "found", Value: "object-42", Receipt: minimalReceipt},
		"queryOptionalFailure":    QueryResult[string]{Branch: "missing", Value: "", Receipt: minimalReceipt, Failure: &failed},
		"mutationHappy":           MutationResult[string]{Branch: "created", Value: "object-42", Receipt: fullReceipt},
		"mutationOptionalFailure": MutationResult[string]{Branch: "rejected", Value: "", Receipt: fullReceipt, Failure: &Failure{Kind: FailureConflict, Provider: "sample", Operation: "createItem", Message: "item already exists"}},
		"mutationUncertain":       MutationResult[string]{Branch: UncertainBranchID, Value: "", Receipt: minimalReceipt, Failure: &unknown},
	})
	kinds := []FailureKind{FailureValidation, FailureAuthentication, FailureAuthorization, FailureNotFound,
		FailureConflict, FailureRateLimit, FailureQuotaExhausted, FailureAvailability, FailureProviderRejection,
		FailureTransport, FailureResponseTooLarge, FailureProtocol, FailureLocalDefect}
	failures := make([]Failure, 0, len(kinds))
	for _, kind := range kinds {
		failure := Failure{Kind: kind, Provider: "sample", Operation: "createItem", Message: "safe example of " + string(kind)}
		if err := failure.Validate(); err != nil {
			t.Fatal(err)
		}
		failures = append(failures, failure)
	}
	conformancefixture.Check(t, "failures.json", failures)
	call := Call{ID: callID, IdempotencyKey: IdempotencyKey(callID), Connection: ConnectionRef{Provider: "sample", Name: "main"}, Operation: OperationRef{ConnectorID: "sample", OperationID: "createItem"}}
	completedReceipt, err := completeReceipt(Receipt{ObservedAt: observedAt}, call)
	if err != nil {
		t.Fatal(err)
	}
	conformancefixture.Check(t, "identity.json", map[string]any{
		"source":                   map[string]string{"flowId": "flow-42", "stepExecutionId": "step-execution-7", "connectorId": "sample", "operationId": "createItem"},
		"derivedCall":              call,
		"differentStepCallId":      deriveCallID("flow-42", "step-execution-8", call.Operation),
		"differentOperationCallId": deriveCallID("flow-42", "step-execution-7", OperationRef{ConnectorID: "sample", OperationID: "getItem"}),
		"fallbackCompletedReceipt": completedReceipt,
		"providerKey":              IdempotencyKey("sample/" + callID),
	})
	conformancefixture.Check(t, "retry-policy.json", DefaultConnectorStepExecuteRetryPolicy())
}
