//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/advertising/go/microsoftadvertisingmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCampaignDiscoveryUsesDefaultMockWithRealDex(t *testing.T) {
	mock := microsoftadvertisingmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	inspectionFlow := NewCampaignInspectionFlow(mock.Connection())
	adGroupFlow := NewAdGroupDiscoveryFlow(mock.Connection())
	keywordFlow := NewKeywordDiscoveryFlow(mock.Connection())
	adFlow := NewAdDiscoveryFlow(mock.Connection())
	createFlow := &adGroupCreationProbeFlow{connection: mock.Connection()}
	keywordCreateFlow := &keywordCreationProbeFlow{connection: mock.Connection()}
	adCreateFlow := &adCreationProbeFlow{connection: mock.Connection()}
	registry, err := dex.NewRegistry([]dex.Flow{flow, inspectionFlow, adGroupFlow, keywordFlow, adFlow, createFlow, keywordCreateFlow, adCreateFlow})
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
	flowID := "microsoftadvertising-campaigndiscovery-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, microsoftadvertising.ListCampaignsInput{AccountID: "12345"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output microsoftadvertising.CampaignsPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "123456789", output.Campaigns[0].ID)
	require.Equal(t, "Sample Search", output.Campaigns[0].Name)
	require.Len(t, mock.ListCampaigns().Calls(), 1)
	inspectionID := "microsoftadvertising-campaigninspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, inspectionFlow, inspectionID, microsoftadvertising.GetCampaignInput{AccountID: "12345", CampaignID: "98765"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	inspection, err := client.WaitForFlow(ctx, inspectionID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, inspection.Status, inspection.ErrorMessage)
	var campaign microsoftadvertising.CampaignSummary
	require.NoError(t, inspection.DecodeSingleOutput(&campaign))
	require.Equal(t, "98765", campaign.ID)
	require.Len(t, mock.GetCampaign().Calls(), 1)
	adGroupID := "microsoftadvertising-adgroups-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, adGroupFlow, adGroupID, microsoftadvertising.ListAdGroupsInput{AccountID: "12345", CampaignID: "98765"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adGroupResult, err := client.WaitForFlow(ctx, adGroupID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adGroupResult.Status, adGroupResult.ErrorMessage)
	var adGroups microsoftadvertising.AdGroupsPage
	require.NoError(t, adGroupResult.DecodeSingleOutput(&adGroups))
	require.Equal(t, "33", adGroups.AdGroups[0].ID)
	require.Len(t, mock.ListAdGroups().Calls(), 1)
	keywordID := "microsoftadvertising-keywords-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, keywordFlow, keywordID, microsoftadvertising.ListKeywordsInput{AccountID: "12345", AdGroupID: "33"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	keywordResult, err := client.WaitForFlow(ctx, keywordID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, keywordResult.Status, keywordResult.ErrorMessage)
	var keywords microsoftadvertising.KeywordsPage
	require.NoError(t, keywordResult.DecodeSingleOutput(&keywords))
	require.Equal(t, "44", keywords.Keywords[0].ID)
	require.Len(t, mock.ListKeywords().Calls(), 1)
	adID := "microsoftadvertising-ads-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, adFlow, adID, microsoftadvertising.ListAdsInput{AccountID: "12345", AdGroupID: "33"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adResult, err := client.WaitForFlow(ctx, adID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, adResult.Status, adResult.ErrorMessage)
	var ads microsoftadvertising.AdsPage
	require.NoError(t, adResult.DecodeSingleOutput(&ads))
	require.Equal(t, "55", ads.Ads[0].ID)
	require.Len(t, mock.ListAds().Calls(), 1)
	mock.CreateAdGroup().Default(microsoftadvertisingmock.CreateAdGroupUncertain(microsoftadvertising.AdGroupMutationResult{}, sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "microsoft-advertising", Operation: "createAdGroup", Message: "create outcome unknown"}))
	createID := "microsoftadvertising-create-uncertain-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, createFlow, createID, microsoftadvertising.CreateAdGroupInput{AccountID: "12345", CampaignID: "98765", Name: "Probe"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	createResult, err := client.WaitForFlow(ctx, createID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, createResult.Status)
	require.Len(t, mock.CreateAdGroup().Calls(), 1)
	mock.CreateKeyword().Default(microsoftadvertisingmock.CreateKeywordUncertain(microsoftadvertising.KeywordMutationResult{}, sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "microsoft-advertising", Operation: "createKeyword", Message: "create outcome unknown"}))
	keywordCreateID := "microsoftadvertising-keyword-create-uncertain-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, keywordCreateFlow, keywordCreateID, microsoftadvertising.CreateKeywordInput{AccountID: "12345", AdGroupID: "33", Text: "Probe", MatchType: "Exact"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	keywordCreateResult, err := client.WaitForFlow(ctx, keywordCreateID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, keywordCreateResult.Status)
	require.Len(t, mock.CreateKeyword().Calls(), 1)
	mock.CreateResponsiveSearchAd().Default(microsoftadvertisingmock.CreateResponsiveSearchAdUncertain(microsoftadvertising.AdMutationResult{}, sdkgo.Failure{Kind: sdkgo.FailureAvailability, Provider: "microsoft-advertising", Operation: "createResponsiveSearchAd", Message: "create outcome unknown"}))
	adCreateID := "microsoftadvertising-ad-create-uncertain-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, adCreateFlow, adCreateID, microsoftadvertising.CreateResponsiveSearchAdInput{AccountID: "12345", AdGroupID: "33", FinalURL: "https://example.com", Headlines: []string{"One", "Two", "Three"}, Descriptions: []string{"First", "Second"}}, dex.StartFlowOptions{})
	require.NoError(t, err)
	adCreateResult, err := client.WaitForFlow(ctx, adCreateID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, adCreateResult.Status)
	require.Len(t, mock.CreateResponsiveSearchAd().Calls(), 1)
}

type adGroupCreationProbeFlow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

func (*adGroupCreationProbeFlow) GetFlowType() string {
	return "MicrosoftAdvertisingAdGroupCreationProbe"
}

func (*adGroupCreationProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}

func (flow *adGroupCreationProbeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdGroupCreationProbe{}),
		dex.DefineStep(microsoftadvertising.NewCreateAdGroupStep(microsoftadvertising.CreateAdGroupStepConfig[microsoftadvertising.CreateAdGroupInput]{
			StepType: "CallCreateAdGroupProbe", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "Create an ad group to verify uncertain outcome routing."},
			Connection:  flow.connection,
			MapToOperationInput: func(input microsoftadvertising.CreateAdGroupInput) microsoftadvertising.CreateAdGroupInput {
				return input
			},
			Created: sdkgo.GoTo(completeAdGroupCreationProbe{}),
		})),
		dex.DefineStep(completeAdGroupCreationProbe{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the ad group input to the provider Step."
type startAdGroupCreationProbe struct{ dex.StepDefaults }

func (startAdGroupCreationProbe) WaitFor(dex.Context, microsoftadvertising.CreateAdGroupInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startAdGroupCreationProbe) Execute(_ dex.Context, input microsoftadvertising.CreateAdGroupInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.CreateAdGroupInput]("CallCreateAdGroupProbe"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after a confirmed ad group creation."
type completeAdGroupCreationProbe struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.CreateAdGroupResult]
}

func (completeAdGroupCreationProbe) Execute(_ dex.Context, result microsoftadvertising.CreateAdGroupResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

type keywordCreationProbeFlow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

func (*keywordCreationProbeFlow) GetFlowType() string {
	return "MicrosoftAdvertisingKeywordCreationProbe"
}
func (*keywordCreationProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}
func (flow *keywordCreationProbeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startKeywordCreationProbe{}),
		dex.DefineStep(microsoftadvertising.NewCreateKeywordStep(microsoftadvertising.CreateKeywordStepConfig[microsoftadvertising.CreateKeywordInput]{
			StepType: "CallCreateKeywordProbe", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "Create a keyword to verify uncertain outcome routing."},
			Connection:  flow.connection,
			MapToOperationInput: func(input microsoftadvertising.CreateKeywordInput) microsoftadvertising.CreateKeywordInput {
				return input
			},
			Created: sdkgo.GoTo(completeKeywordCreationProbe{}),
		})),
		dex.DefineStep(completeKeywordCreationProbe{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the keyword input to the provider Step."
type startKeywordCreationProbe struct{ dex.StepDefaults }

func (startKeywordCreationProbe) WaitFor(dex.Context, microsoftadvertising.CreateKeywordInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startKeywordCreationProbe) Execute(_ dex.Context, input microsoftadvertising.CreateKeywordInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.CreateKeywordInput]("CallCreateKeywordProbe"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after a confirmed keyword creation."
type completeKeywordCreationProbe struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.CreateKeywordResult]
}

func (completeKeywordCreationProbe) Execute(_ dex.Context, result microsoftadvertising.CreateKeywordResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

type adCreationProbeFlow struct {
	dex.FlowDefaults
	connection microsoftadvertising.Connection
}

func (*adCreationProbeFlow) GetFlowType() string { return "MicrosoftAdvertisingAdCreationProbe" }
func (*adCreationProbeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{}
}
func (flow *adCreationProbeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdCreationProbe{}),
		dex.DefineStep(microsoftadvertising.NewCreateResponsiveSearchAdStep(microsoftadvertising.CreateResponsiveSearchAdStepConfig[microsoftadvertising.CreateResponsiveSearchAdInput]{
			StepType: "CallCreateAdProbe", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft Advertising", Explanation: "Create an ad to verify uncertain outcome routing."},
			Connection:  flow.connection,
			MapToOperationInput: func(input microsoftadvertising.CreateResponsiveSearchAdInput) microsoftadvertising.CreateResponsiveSearchAdInput {
				return input
			},
			Created: sdkgo.GoTo(completeAdCreationProbe{}),
		})),
		dex.DefineStep(completeAdCreationProbe{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the ad input to the provider Step."
type startAdCreationProbe struct{ dex.StepDefaults }

func (startAdCreationProbe) WaitFor(dex.Context, microsoftadvertising.CreateResponsiveSearchAdInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdCreationProbe) Execute(_ dex.Context, input microsoftadvertising.CreateResponsiveSearchAdInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[microsoftadvertising.CreateResponsiveSearchAdInput]("CallCreateAdProbe"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after a confirmed ad creation."
type completeAdCreationProbe struct {
	dex.StepDefaultsNoWaitFor[microsoftadvertising.CreateResponsiveSearchAdResult]
}

func (completeAdCreationProbe) Execute(_ dex.Context, result microsoftadvertising.CreateResponsiveSearchAdResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}
