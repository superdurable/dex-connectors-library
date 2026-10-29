//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadqualification

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm/crmmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestLeadQualificationExampleRunsOnCRMMockWithRealDex(t *testing.T) {
	mock := crmmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	require.NoError(t, worker.WaitPullRegistered(t.Context()))
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "zoho-crm-lead-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	mock.FindRecords().When(func(input crm.FindRecordsInput) bool { return input.Module == crm.ModuleDeals }, crmmock.FindRecordsOpenDeal())
	request := Input{ContactEmail: " ada@acme.example.com ", ContactFirstName: " Ada ", ContactLastName: " Lovelace ", AccountName: " Acme Corp ", Stage: " Negotiation/Review ", OwnerID: " 4150868000000376001 "}
	_, err = client.StartFlow(ctx, flow, flowID, request, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var qualification LeadQualification
	require.NoError(t, result.DecodeSingleOutput(&qualification))
	require.Equal(t, PhaseDealAdvanced, qualification.Phase)
	require.Equal(t, "ada@acme.example.com", qualification.Request.ContactEmail)
	require.Equal(t, "Acme Corp", qualification.Request.AccountName)
	require.Equal(t, "Negotiation/Review", qualification.DealStage)
	require.Equal(t, "4150868000000376001", qualification.DealOwnerID)
	require.False(t, qualification.IsAccountCreated)
	require.True(t, qualification.IsContactCreated)
	require.Equal(t, "4150868000000376008", qualification.AccountID)
	require.Equal(t, "4150868000000376011", qualification.ContactID)
	require.Equal(t, "4150868000000376022", qualification.DealID)

	fieldCalls := mock.ListModuleFields().Calls()
	require.Len(t, fieldCalls, 1)
	require.Equal(t, flowID, fieldCalls[0].FlowID)
	require.Equal(t, crm.ListModuleFieldsInput{Module: "Deals"}, fieldCalls[0].Input)
	findCalls := mock.FindRecords().Calls()
	require.Len(t, findCalls, 2)
	require.Equal(t, crm.FindRecordsInput{
		Module: "Accounts", Fields: []string{"Account_Name"},
		Conditions: []crm.RecordCondition{{Field: "Account_Name", Operator: "=", Value: &crm.COQLValue{Kind: "text", Text: "Acme Corp"}}},
		Limit:      2,
	}, findCalls[0].Input)
	require.Equal(t, crm.FindRecordsInput{
		Module: "Deals", Fields: []string{"Deal_Name", "Stage", "Owner", "Modified_Time"},
		Conditions: []crm.RecordCondition{
			{Field: "Contact_Name", Operator: "=", Value: &crm.COQLValue{Kind: "recordId", Text: "4150868000000376011"}},
			{Field: "Stage", Operator: "not in", Value: &crm.COQLValue{Kind: "textList", Texts: []string{"Closed Won", "Closed Lost", "Closed Lost to Competition"}}},
		},
		Sort: &crm.RecordSort{Field: "Modified_Time", IsDescending: true}, Limit: 1,
	}, findCalls[1].Input)
	upsertCalls := mock.UpsertRecord().Calls()
	require.Len(t, upsertCalls, 1)
	require.Equal(t, crm.UpsertRecordInput{
		Module: "Contacts", DuplicateCheckFields: []string{"Email"},
		Fields: map[string]json.RawMessage{
			"Email": json.RawMessage(`"ada@acme.example.com"`), "First_Name": json.RawMessage(`"Ada"`),
			"Last_Name": json.RawMessage(`"Lovelace"`), "Account_Name": json.RawMessage(`{"id":"4150868000000376008"}`),
		},
	}, upsertCalls[0].Input)
	updateCalls := mock.UpdateRecord().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, crm.UpdateRecordInput{
		Module: "Deals", RecordID: "4150868000000376022",
		Fields: map[string]json.RawMessage{
			"Stage": json.RawMessage(`"Negotiation/Review"`),
			"Owner": json.RawMessage(`{"id":"4150868000000376001"}`),
		},
	}, updateCalls[0].Input)
	readCalls := mock.GetRecord().Calls()
	require.Len(t, readCalls, 1)
	require.Equal(t, crm.GetRecordInput{Module: "Deals", RecordID: "4150868000000376022", Fields: []string{"Deal_Name", "Stage", "Owner", "Contact_Name", "Modified_Time"}}, readCalls[0].Input)
}
