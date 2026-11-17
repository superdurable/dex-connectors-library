//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/connectors/keap/go/keapmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestContactReadUsesDefaultMockWithRealDex(t *testing.T) {
	mock := keapmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, NewDirectoryFlow(mock.Connection()), NewCompanyFlow(mock.Connection()), NewNoteFlow(mock.Connection()), NewTagFlow(mock.Connection()), NewOrderFlow(mock.Connection()), NewProductFlow(mock.Connection())})
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
	flowID := "keap-contactread-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, keap.GetContactInput{ContactID: "123"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output keap.ContactProfile
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "123", output.ID)
	require.Equal(t, "Example", output.GivenName)
	require.Len(t, mock.GetContact().Calls(), 1)
	directoryFlow := NewDirectoryFlow(mock.Connection())
	directoryID := "keap-directory-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, directoryFlow, directoryID, keap.ListContactsInput{PageSize: 20}, dex.StartFlowOptions{})
	require.NoError(t, err)
	directoryResult, err := client.WaitForFlow(ctx, directoryID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, directoryResult.Status, directoryResult.ErrorMessage)
	var page keap.ContactsPage
	require.NoError(t, directoryResult.DecodeSingleOutput(&page))
	require.Len(t, page.Contacts, 1)
	require.Equal(t, "123", page.Contacts[0].ID)
	require.Len(t, mock.ListContacts().Calls(), 1)
	companyFlow := NewCompanyFlow(mock.Connection())
	companyFlowID := "keap-company-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, companyFlow, companyFlowID, keap.CompanyIDInput{CompanyID: "321"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	companyResult, err := client.WaitForFlow(ctx, companyFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, companyResult.Status, companyResult.ErrorMessage)
	var company keap.CompanyProfile
	require.NoError(t, companyResult.DecodeSingleOutput(&company))
	require.Equal(t, "321", company.ID)
	require.Len(t, mock.GetCompany().Calls(), 1)
	noteFlow := NewNoteFlow(mock.Connection())
	noteFlowID := "keap-note-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, noteFlow, noteFlowID, keap.ContactNoteIDInput{ContactID: "123", NoteID: "456"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	noteResult, err := client.WaitForFlow(ctx, noteFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, noteResult.Status, noteResult.ErrorMessage)
	var note keap.ContactNote
	require.NoError(t, noteResult.DecodeSingleOutput(&note))
	require.Equal(t, "456", note.ID)
	require.Len(t, mock.GetContactNote().Calls(), 1)
	tagFlow := NewTagFlow(mock.Connection())
	tagFlowID := "keap-tags-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, tagFlow, tagFlowID, keap.ListContactTagsInput{ContactID: "123", PageSize: 20}, dex.StartFlowOptions{})
	require.NoError(t, err)
	tagResult, err := client.WaitForFlow(ctx, tagFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, tagResult.Status, tagResult.ErrorMessage)
	var tags keap.ContactTagsPage
	require.NoError(t, tagResult.DecodeSingleOutput(&tags))
	require.Len(t, tags.Tags, 1)
	require.Equal(t, "567", tags.Tags[0].ID)
	require.Len(t, mock.ListContactTags().Calls(), 1)
	orderFlow := NewOrderFlow(mock.Connection())
	orderFlowID := "keap-order-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, orderFlow, orderFlowID, keap.OrderIDInput{OrderID: "678"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	orderResult, err := client.WaitForFlow(ctx, orderFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, orderResult.Status, orderResult.ErrorMessage)
	var order keap.OrderProfile
	require.NoError(t, orderResult.DecodeSingleOutput(&order))
	require.Equal(t, "678", order.ID)
	require.Len(t, mock.GetOrder().Calls(), 1)

	productFlow := NewProductFlow(mock.Connection())
	productFlowID := "keap-product-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, productFlow, productFlowID, keap.ProductIDInput{ProductID: "901"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	productResult, err := client.WaitForFlow(ctx, productFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, productResult.Status, productResult.ErrorMessage)
	var product keap.ProductProfile
	require.NoError(t, productResult.DecodeSingleOutput(&product))
	require.Equal(t, "901", product.ID)
	require.Len(t, mock.GetProduct().Calls(), 1)
}
