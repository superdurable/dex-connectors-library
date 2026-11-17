//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/connectors/creatio/go/creatiomock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := creatiomock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	searchFlow := NewContactsSearchFlow(mock.Connection())
	creationFlow := NewContactCreationFlow(mock.Connection())
	accountSearchFlow := NewAccountsSearchFlow(mock.Connection())
	accountCreationFlow := NewAccountCreationFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, searchFlow, creationFlow, accountSearchFlow, accountCreationFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "creatio-contactinspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, creatio.GetContactInput{ContactID: "90a68db5-cb3a-4f64-bb60-c0a934ee7600"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output creatio.ContactSummary
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "90a68db5-cb3a-4f64-bb60-c0a934ee7600", output.ID)
	require.Equal(t, "Ada Lovelace", output.Name)
	require.Equal(t, "ada@example.com", output.Email)
	require.Len(t, mock.GetContact().Calls(), 1)
	searchFlowID := "creatio-contacts-search-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, searchFlow, searchFlowID, creatio.ListContactsInput{PageNumber: 1, PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	searchResult, err := client.WaitForFlow(ctx, searchFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, searchResult.Status, searchResult.ErrorMessage)
	var contactsPage creatio.ContactsPage
	require.NoError(t, searchResult.DecodeSingleOutput(&contactsPage))
	require.Len(t, contactsPage.Contacts, 1)
	require.Len(t, mock.ListContacts().Calls(), 1)
	creationFlowID := "creatio-contact-creation-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, creationFlow, creationFlowID, creatio.CreateContactInput{Name: "Ada Lovelace", Email: "ada@example.com"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	creationResult, err := client.WaitForFlow(ctx, creationFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, creationResult.Status, creationResult.ErrorMessage)
	var createdContact creatio.ContactSummary
	require.NoError(t, creationResult.DecodeSingleOutput(&createdContact))
	require.Equal(t, "90a68db5-cb3a-4f64-bb60-c0a934ee7600", createdContact.ID)
	require.Len(t, mock.CreateContact().Calls(), 1)
	accountSearchFlowID := "creatio-accounts-search-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, accountSearchFlow, accountSearchFlowID, creatio.ListAccountsInput{PageNumber: 1, PageSize: 25}, dex.StartFlowOptions{})
	require.NoError(t, err)
	accountSearchResult, err := client.WaitForFlow(ctx, accountSearchFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, accountSearchResult.Status, accountSearchResult.ErrorMessage)
	var accountsPage creatio.AccountsPage
	require.NoError(t, accountSearchResult.DecodeSingleOutput(&accountsPage))
	require.Len(t, accountsPage.Accounts, 1)
	require.Len(t, mock.ListAccounts().Calls(), 1)
	accountCreationFlowID := "creatio-account-creation-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, accountCreationFlow, accountCreationFlowID, creatio.CreateAccountInput{Name: "Example Company"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	accountCreationResult, err := client.WaitForFlow(ctx, accountCreationFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, accountCreationResult.Status, accountCreationResult.ErrorMessage)
	var createdAccount creatio.AccountSummary
	require.NoError(t, accountCreationResult.DecodeSingleOutput(&createdAccount))
	require.Equal(t, "59f2ffca-b4ae-4545-82e6-58830db5077b", createdAccount.ID)
	require.Len(t, mock.CreateAccount().Calls(), 1)
}
