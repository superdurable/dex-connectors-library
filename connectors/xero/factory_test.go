// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// xeroIdempotencyKeyLifetime is how long Xero caches a response for one Idempotency-Key.
const xeroIdempotencyKeyLifetime = 6 * time.Minute

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "xero", GroupLabel: "Xero", Explanation: "Call Xero."}
	require.NotPanics(t, func() {
		xero.NewListContactsStep(xero.ListContactsStepConfig[string]{
			StepType: "ListContacts", Annotations: annotations, Connection: connection, ConnectionName: xeroConnection.Name,
			MapToOperationInput: func(term string) xero.ListContactsInput { return xero.ListContactsInput{SearchTerm: term} },
			Listed:              sdkgo.GoTo(completeTarget[xero.ListContactsResult]{}),
		})
		xero.NewFindContactByEmailStep(xero.FindContactByEmailStepConfig[string]{
			StepType: "FindContact", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(email string) xero.FindContactByEmailInput {
				return xero.FindContactByEmailInput{EmailAddress: email}
			},
			Found: sdkgo.GoTo(completeTarget[xero.FindContactByEmailResult]{}),
		})
		xero.NewGetInvoiceStep(xero.GetInvoiceStepConfig[string]{
			StepType: "GetInvoice", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id string) xero.GetInvoiceInput { return xero.GetInvoiceInput{InvoiceID: id} },
			Found:               sdkgo.GoTo(completeTarget[xero.GetInvoiceResult]{}),
		})
		xero.NewListInvoicesStep(xero.ListInvoicesStepConfig[string]{
			StepType: "ListInvoices", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(reference string) xero.ListInvoicesInput { return xero.ListInvoicesInput{Reference: reference} },
			Listed:              sdkgo.GoTo(completeTarget[xero.ListInvoicesResult]{}),
		})
		xero.NewCreateInvoiceStep(xero.CreateInvoiceStepConfig[string]{
			StepType: "CreateInvoice", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(string) xero.CreateInvoiceInput { return validCreateInvoiceInput() },
			Created:             sdkgo.GoTo(completeTarget[xero.CreateInvoiceResult]{}),
		})
		xero.NewRecordPaymentStep(xero.RecordPaymentStepConfig[string]{
			StepType: "RecordPayment", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(string) xero.RecordPaymentInput { return validRecordPaymentInput() },
			Recorded:            sdkgo.GoTo(completeTarget[xero.RecordPaymentResult]{}),
		})
	})
	require.Panics(t, func() {
		xero.NewRecordPaymentStep(xero.RecordPaymentStepConfig[string]{
			StepType: "RecordPayment", Connection: connection,
			MapToOperationInput: func(string) xero.RecordPaymentInput { return validRecordPaymentInput() },
			Uncertain:           sdkgo.GoTo(completeTarget[xero.RecordPaymentResult]{}),
		})
	}, "recorded is the required branch")
	require.Panics(t, func() {
		xero.NewGetInvoiceStep(xero.GetInvoiceStepConfig[string]{
			StepType: "GetInvoice", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) xero.GetInvoiceInput { return xero.GetInvoiceInput{InvoiceID: id} },
			Found:               sdkgo.GoTo(completeTarget[xero.GetInvoiceResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndKeyedAsyncDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "dailyLimitReached": true, "invalidResponse": true, "defect": true},
		branchOptionality(xero.ListContactsDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{
		"found": false, "notFound": true, "ambiguous": true, "providerRejected": true, "dailyLimitReached": true, "invalidResponse": true, "defect": true,
	}, branchOptionality(xero.FindContactByEmailDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{
		"found": false, "notFound": true, "providerRejected": true, "dailyLimitReached": true, "invalidResponse": true, "defect": true,
	}, branchOptionality(xero.GetInvoiceDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "dailyLimitReached": true, "invalidResponse": true, "defect": true},
		branchOptionality(xero.ListInvoicesDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "dailyLimitReached": true, "uncertain": true, "defect": true},
		branchOptionality(xero.CreateInvoiceDefinition.Branches), "uncertain covers a cached 500 and an unusable accepted answer")
	require.Equal(t, map[sdkgo.BranchID]bool{"recorded": false, "providerRejected": true, "dailyLimitReached": true, "uncertain": true, "defect": true},
		branchOptionality(xero.RecordPaymentDefinition.Branches))
	reads := map[string]sdkgo.StepDefaults{
		"listContacts": xero.ListContactsDefinition.StepDefaults, "findContactByEmail": xero.FindContactByEmailDefinition.StepDefaults,
		"getInvoice": xero.GetInvoiceDefinition.StepDefaults, "listInvoices": xero.ListInvoicesDefinition.StepDefaults,
	}
	writes := map[string]sdkgo.StepDefaults{
		"createInvoice": xero.CreateInvoiceDefinition.StepDefaults, "recordPayment": xero.RecordPaymentDefinition.StepDefaults,
	}
	for name, defaults := range reads {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, name)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute, "%s: a one-minute limit reset fits in the window", name)
	}
	for name, defaults := range writes {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "%s: a duplicate dispatch replays under the Idempotency-Key", name)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, name)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute, name)
		require.Less(t, defaults.ExecuteRetry.TotalDuration+defaults.ExecuteMethodTimeout, xeroIdempotencyKeyLifetime,
			"%s: every attempt must reach Xero while it still caches the first response", name)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAccessToken)
	credentials := xero.Credentials{
		AuthMethodID: xero.OAuthAuthMethodID, ClientSecret: sdkgo.NewSecretString(testAccessToken),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString(testAccessToken),
	}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAccessToken)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) xero.Connection {
	t.Helper()
	client, err := xero.New(xero.Config{}, customConnectionCredentials())
	require.NoError(t, err)
	connection, err := xero.NewConnection(client, xeroConnection)
	require.NoError(t, err)
	return connection
}
