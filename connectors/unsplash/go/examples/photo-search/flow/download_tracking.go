// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package photosearch

import (
	"github.com/superdurable/dex-connectors-library/connectors/unsplash/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DownloadTrackingFlowType identifies one photo download tracking Flow.
const DownloadTrackingFlowType = "UnsplashDownloadTracking"

var trackedDownloadAttribute = dex.DefineAttribute[unsplash.TrackedPhotoDownload]("unsplash-tracked-download")

// DownloadTrackingInput identifies the photo selected for a download-like use.
type DownloadTrackingInput struct {
	// PhotoID is the Unsplash photo ID returned by a search or lookup.
	PhotoID string `json:"photoId"`
}

// DownloadTrackingFlow records one photo download event at Unsplash.
type DownloadTrackingFlow struct {
	dex.FlowDefaults
	connection unsplash.Connection
}

// NewDownloadTrackingFlow binds the Unsplash connection for the event.
func NewDownloadTrackingFlow(connection unsplash.Connection) *DownloadTrackingFlow {
	return &DownloadTrackingFlow{connection: connection}
}

// GetFlowType returns DownloadTrackingFlowType.
func (*DownloadTrackingFlow) GetFlowType() string { return DownloadTrackingFlowType }

// GetPersistenceSchema registers the tracked image URL Attribute.
func (*DownloadTrackingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{trackedDownloadAttribute}}
}

// GetSteps declares the start, provider event, and completion Steps.
func (flow *DownloadTrackingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDownloadTracking{}),
		dex.DefineStep(unsplash.NewTrackPhotoDownloadStep(unsplash.TrackPhotoDownloadStepConfig[DownloadTrackingInput]{
			StepType: "TrackPhotoDownload", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "downloads", GroupLabel: "Downloads", Explanation: "Record the selected photo's download event with Unsplash."},
			MapToOperationInput: func(input DownloadTrackingInput) unsplash.TrackPhotoDownloadInput {
				return unsplash.TrackPhotoDownloadInput{ID: input.PhotoID}
			},
			Tracked: sdkgo.GoTo(completeDownloadTracking{}),
		})),
		dex.DefineStep(completeDownloadTracking{}),
	}
}

// GetRPCs exposes the tracked image URL in Dex Web.
func (flow *DownloadTrackingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the recorded image URL in the run list.
// dex:field attribute-key:unsplash-tracked-download value-type:json editable:false description:"Tracked Unsplash image URL"
func (*DownloadTrackingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := trackedDownloadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unsplash-tracked-download": value}}, nil
}

// GetDexDisplay returns the recorded image URL in run detail.
// dex:field attribute-key:unsplash-tracked-download value-type:json editable:false description:"URL returned after Unsplash recorded the download"
func (*DownloadTrackingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := trackedDownloadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"unsplash-tracked-download": value}}, nil
}

// dex:group group-id:downloads group-label:"Downloads"
// dex:explanation text:"Validate the chosen photo before requesting Unsplash download tracking."
type startDownloadTracking struct {
	dex.StepDefaultsNoWaitFor[DownloadTrackingInput]
}

func (startDownloadTracking) GetStepType() string { return "StartDownloadTracking" }
func (startDownloadTracking) Execute(_ dex.Context, input DownloadTrackingInput) (*dex.StepDecision, error) {
	if input.PhotoID == "" {
		return dex.ForceFail("photoId is required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[DownloadTrackingInput]("TrackPhotoDownload"), input), nil
}

// dex:group group-id:downloads group-label:"Downloads"
// dex:explanation text:"Persist the URL Unsplash returned after recording the photo download."
type completeDownloadTracking struct {
	dex.StepDefaultsNoWaitFor[unsplash.TrackPhotoDownloadResult]
}

func (completeDownloadTracking) GetStepType() string { return "CompleteDownloadTracking" }
func (completeDownloadTracking) Execute(ctx dex.Context, result unsplash.TrackPhotoDownloadResult) (*dex.StepDecision, error) {
	if err := trackedDownloadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DownloadTrackingFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*DownloadTrackingFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*DownloadTrackingFlow)(nil).GetDexDisplay
