// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

// GetStepType names the target: Dex gives a generic Step type no default name.
func (completeTarget[IN]) GetStepType() string { return "Complete" }

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "quickbooks", GroupLabel: "QuickBooks Online", Explanation: "Call QuickBooks."}
	name := quickbooksConnection.Name
	require.NotPanics(t, func() {
		quickbooks.NewFindCustomerStep(quickbooks.FindCustomerStepConfig[string]{
			StepType: "FindCustomer", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(email string) quickbooks.FindCustomerInput {
				return quickbooks.FindCustomerInput{EmailAddress: email}
			},
			Found: sdkgo.GoTo(completeTarget[quickbooks.FindCustomerResult]{}),
		})
		quickbooks.NewCreateCustomerStep(quickbooks.CreateCustomerStepConfig[string]{
			StepType: "CreateCustomer", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(displayName string) quickbooks.CreateCustomerInput {
				return quickbooks.CreateCustomerInput{DisplayName: displayName}
			},
			Created: sdkgo.GoTo(completeTarget[quickbooks.CreateCustomerResult]{}),
		})
		quickbooks.NewCreateInvoiceStep(quickbooks.CreateInvoiceStepConfig[string]{
			StepType: "CreateInvoice", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) quickbooks.CreateInvoiceInput { return validCreateInvoiceInput() },
			Created:             sdkgo.GoTo(completeTarget[quickbooks.CreateInvoiceResult]{}),
		})
		quickbooks.NewGetInvoiceStep(quickbooks.GetInvoiceStepConfig[string]{
			StepType: "GetInvoice", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(id string) quickbooks.GetInvoiceInput { return quickbooks.GetInvoiceInput{InvoiceID: id} },
			Found:               sdkgo.GoTo(completeTarget[quickbooks.GetInvoiceResult]{}),
		})
		quickbooks.NewListInvoicesStep(quickbooks.ListInvoicesStepConfig[string]{
			StepType: "ListInvoices", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(customerID string) quickbooks.ListInvoicesInput {
				return quickbooks.ListInvoicesInput{CustomerID: customerID}
			},
			Listed: sdkgo.GoTo(completeTarget[quickbooks.ListInvoicesResult]{}),
		})
		quickbooks.NewSendInvoiceStep(quickbooks.SendInvoiceStepConfig[string]{
			StepType: "SendInvoice", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(id string) quickbooks.SendInvoiceInput { return quickbooks.SendInvoiceInput{InvoiceID: id} },
			Sent:                sdkgo.GoTo(completeTarget[quickbooks.SendInvoiceResult]{}),
		})
		quickbooks.NewRecordPaymentStep(quickbooks.RecordPaymentStepConfig[string]{
			StepType: "RecordPayment", Annotations: annotations, Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) quickbooks.RecordPaymentInput { return validRecordPaymentInput() },
			Recorded:            sdkgo.GoTo(completeTarget[quickbooks.RecordPaymentResult]{}),
		})
	})
	require.Panics(t, func() {
		quickbooks.NewRecordPaymentStep(quickbooks.RecordPaymentStepConfig[string]{
			StepType: "RecordPayment", Connection: connection, ConnectionName: name,
			MapToOperationInput: func(string) quickbooks.RecordPaymentInput { return validRecordPaymentInput() },
			Uncertain:           sdkgo.GoTo(completeTarget[quickbooks.RecordPaymentResult]{}),
		})
	}, "recorded is the required branch")
	require.Panics(t, func() {
		quickbooks.NewGetInvoiceStep(quickbooks.GetInvoiceStepConfig[string]{
			StepType: "GetInvoice", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(id string) quickbooks.GetInvoiceInput { return quickbooks.GetInvoiceInput{InvoiceID: id} },
			Found:               sdkgo.GoTo(completeTarget[quickbooks.GetInvoiceResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndKeyedAsyncDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{
		"found": false, "notFound": true, "ambiguous": true, "providerRejected": true, "invalidResponse": true, "defect": true,
	}, branchOptionality(quickbooks.FindCustomerDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "nameConflict": true, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(quickbooks.CreateCustomerDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(quickbooks.CreateInvoiceDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(quickbooks.GetInvoiceDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(quickbooks.ListInvoicesDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"sent": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(quickbooks.SendInvoiceDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"recorded": false, "providerRejected": true, "uncertain": true, "defect": true},
		branchOptionality(quickbooks.RecordPaymentDefinition.Branches))
	for name, defaults := range map[string]sdkgo.StepDefaults{
		"findCustomer": quickbooks.FindCustomerDefinition.StepDefaults, "getInvoice": quickbooks.GetInvoiceDefinition.StepDefaults,
		"listInvoices": quickbooks.ListInvoicesDefinition.StepDefaults, "createCustomer": quickbooks.CreateCustomerDefinition.StepDefaults,
		"createInvoice": quickbooks.CreateInvoiceDefinition.StepDefaults, "sendInvoice": quickbooks.SendInvoiceDefinition.StepDefaults,
		"recordPayment": quickbooks.RecordPaymentDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "%s: a duplicate dispatch replays under the requestid", name)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, name)
		require.GreaterOrEqual(t, defaults.ExecuteRetry.TotalDuration, 2*time.Minute, "%s: the documented one-minute throttle wait fits", name)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAccessToken)
	credentials := quickbooks.Credentials{
		ClientSecret: sdkgo.NewSecretString(testAccessToken), AccessToken: sdkgo.NewSecretString(testAccessToken),
		RefreshToken: sdkgo.NewSecretString(testAccessToken), IDToken: sdkgo.NewSecretString(testAccessToken),
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

func newTestConnection(t *testing.T) quickbooks.Connection {
	t.Helper()
	client, err := quickbooks.New(quickbooks.Config{}, staticCredentials(testIDToken(t, `"realmid":"`+testRealmID+`"`)))
	require.NoError(t, err)
	connection, err := quickbooks.NewConnection(client, quickbooksConnection)
	require.NoError(t, err)
	return connection
}
