// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package playlistcurator

import (
	"errors"

	youtube "github.com/superdurable/dex-connectors-library/connectors/google/youtube/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const playlistInspectionFlowType = "YouTubePlaylistInspection"
const getPlaylistStepType = "CallGetPlaylist"

var inspectedPlaylistAttribute = dex.DefineAttribute[youtube.Playlist]("inspected-youtube-playlist")

// PlaylistInspectionFlow reads one playlist through the operation-specific GetPlaylist Step.
type PlaylistInspectionFlow struct {
	dex.FlowDefaults
	connection youtube.Connection
}

// NewPlaylistInspectionFlow binds the read-only YouTube connection.
func NewPlaylistInspectionFlow(connection youtube.Connection) *PlaylistInspectionFlow {
	return &PlaylistInspectionFlow{connection: connection}
}

// GetFlowType preserves this example Flow identity.
func (*PlaylistInspectionFlow) GetFlowType() string { return playlistInspectionFlowType }

// GetPersistenceSchema registers the inspected playlist Attribute.
func (*PlaylistInspectionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inspectedPlaylistAttribute}}
}

// GetSteps registers the typed lookup and completion Steps.
func (flow *PlaylistInspectionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPlaylistInspection{}),
		dex.DefineStep(youtube.NewGetPlaylistStep(youtube.GetPlaylistStepConfig[youtube.GetPlaylistInput]{
			StepType: getPlaylistStepType, ConnectionName: ReadConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "YouTube", Explanation: "Read the selected playlist's metadata."},
			Connection:          flow.connection,
			MapToOperationInput: mapPlaylistID,
			Found:               sdkgo.GoTo(completePlaylistInspection{}),
		})),
		dex.DefineStep(completePlaylistInspection{}),
	}
}

// GetRPCs provides Dex Web summary and display views.
func (flow *PlaylistInspectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:inspected-youtube-playlist value-type:json editable:false description:"Playlist metadata"
// GetDexSummary returns the inspected playlist metadata.
func (*PlaylistInspectionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	playlist, err := readInspectedPlaylist(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"inspected-youtube-playlist": playlist}}, nil
}

// dex:field attribute-key:inspected-youtube-playlist value-type:json editable:false description:"Playlist metadata"
// GetDexDisplay returns the inspected playlist metadata.
func (*PlaylistInspectionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	playlist, err := readInspectedPlaylist(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"inspected-youtube-playlist": playlist}}, nil
}

func readInspectedPlaylist(ctx dex.Context) (youtube.Playlist, error) {
	playlist, err := inspectedPlaylistAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return youtube.Playlist{}, nil
	}
	return playlist, err
}

func mapPlaylistID(input youtube.GetPlaylistInput) youtube.GetPlaylistInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass a playlist ID to the YouTube lookup Step."
type startPlaylistInspection struct{ dex.StepDefaults }

func (startPlaylistInspection) GetStepType() string { return "StartPlaylistInspection" }
func (startPlaylistInspection) WaitFor(dex.Context, youtube.GetPlaylistInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPlaylistInspection) Execute(_ dex.Context, input youtube.GetPlaylistInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[youtube.GetPlaylistInput](getPlaylistStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the inspected playlist metadata."
type completePlaylistInspection struct {
	dex.StepDefaultsNoWaitFor[youtube.GetPlaylistResult]
}

func (completePlaylistInspection) GetStepType() string { return "CompletePlaylistInspection" }
func (completePlaylistInspection) Execute(ctx dex.Context, result youtube.GetPlaylistResult) (*dex.StepDecision, error) {
	if err := inspectedPlaylistAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PlaylistInspectionFlow)(nil)
