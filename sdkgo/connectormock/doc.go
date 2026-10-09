// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package connectormock scripts connector operations for application tests
// that run Flows on a real Dex Worker without calling a provider.
//
// Query and Mutation implement sdkgo.Query and sdkgo.Mutation with the
// operation's real definition. The SDK validates every scripted answer as it
// validates a provider connector, so a Step factory accepts the mock and an
// undeclared branch, an invalid failure, or a misplaced uncertain case still
// selects the defect branch. Most tests use a connector's generated mock
// package, such as llm/llmmock, which builds these mocks for every operation,
// installs the manifest default, and returns a typed Connection. A generic
// Step can use a mock directly, as in
// sdkgo/integrationtest/connector_mock_integration_test.go:
//
//	checkStock := connectormock.NewQuery[stockRequest, stockLevel](t, checkStockDefinition)
//	checkStock.Respond(
//		connectormock.Retry[stockLevel](sdkgo.Failure{Kind: sdkgo.FailureAvailability, Message: "warehouse busy"}, 0),
//		connectormock.Branch(mockedInventoryFound, stockLevel{SKU: "sku-1", Quantity: 7}, nil),
//	)
//	flow := mockedStockFlow{checkStock: checkStock}
//
// where the Flow passes the mock as the Step's operation:
//
//	check := sdkgo.MustNewQueryStep(sdkgo.QueryStepConfig[stockRequest, stockRequest, stockLevel]{
//		StepType:            "CheckMockedStock",
//		Annotations:         sdkgo.StepAnnotations{GroupID: "inventory", GroupLabel: "Inventory", Explanation: "Check the item's stock."},
//		Operation:           flow.checkStock,
//		Connection:          mockedInventoryConnection,
//		MapToOperationInput: func(request stockRequest) stockRequest { return request },
//		Branches: []sdkgo.BranchTarget[sdkgo.QueryResult[stockLevel]]{
//			sdkgo.GoToBranch(mockedInventoryFound, stockFoundStep{}),
//			sdkgo.GoToBranch(mockedInventoryMissing, stockMissingStep{}),
//		},
//	})
//
// The Retry asks Dex to retry the Step, and the retried attempt takes the next
// case. Calls then reports both calls with the Step execution's Call ID.
//
// # Scripting
//
// Respond queues cases in order, When adds a persistent rule for matching
// inputs, ForFlow scopes either to the Flows whose ID starts with a value,
// Default sets the cases used when nothing else answers, and Strict ignores
// every Default. A call that nothing answers fails the test with t.Errorf and
// selects the defect branch. Scripting mistakes, such as a branch the
// operation does not declare, also fail the test when they are scripted.
//
// # Cases
//
// Branch selects a declared branch with a value and an optional failure.
// Retry asks Dex to retry the Step. Uncertain selects the standard uncertain
// branch of a Mutation that declares it. Paged answers one page of a listing
// per call, chosen from the input. A failure with an empty Provider or
// Operation is completed with the operation's identity.
//
// # Idempotency and records
//
// A Mutation replays the first terminal outcome for a repeated idempotency
// key without consuming a case, so a Step whose Execute is retried after the
// call observes one provider effect. Calls returns each mock's calls with
// their input, and RecordedCalls returns every call answered in the process
// without inputs, so a test harness can check that every wired branch was
// exercised.
//
// Mocks never write progress or text Streams and never resolve credentials.
package connectormock
