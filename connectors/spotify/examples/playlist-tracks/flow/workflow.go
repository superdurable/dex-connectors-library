// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package playlisttracks demonstrates the Spotify listPlaylistTracks Query in a Flow
// started from Dex Web Start Flow.
package playlisttracks

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/spotify"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "SpotifyPlaylistTracks"
	// ConnectionName is the static Dex Web connection that holds Spotify authorization.
	ConnectionName = "spotify-playlist-reader"

	listPlaylistTracksStepType     = "ListPlaylistTracks"
	recordPlaylistRequestStepType  = "RecordPlaylistRequest"
	completePlaylistTracksStepType = "CompletePlaylistTracks"
)

var (
	playlistRequestAttribute = dex.DefineAttribute[Input]("spotify-playlist-tracks-request")
	playlistPageAttribute    = dex.DefineAttribute[spotify.PlaylistTrackPage]("spotify-playlist-tracks-page")
)

// Input is the typed playlist page request entered in Dex Web Start Flow.
type Input struct {
	// PlaylistID is the 22-character Spotify playlist identifier.
	PlaylistID string `json:"playlistId"`
	// Market is an optional ISO 3166-1 alpha-2 country code.
	Market string `json:"market,omitempty"`
	// Limit is 1 through 50. Zero selects 20.
	Limit int `json:"limit,omitempty"`
	// Offset is the zero-based playlist item offset.
	Offset int `json:"offset,omitempty"`
}

// Flow lists one bounded page of Spotify playlist tracks.
type Flow struct {
	dex.FlowDefaults
	connection spotify.Connection
}

// NewFlow binds the Spotify Connection at registration time.
func NewFlow(connection spotify.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the Flow's request, connector, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordPlaylistRequest{}),
		dex.DefineStep(spotify.NewListPlaylistTracksStep(spotify.ListPlaylistTracksStepConfig[Input]{
			StepType: listPlaylistTracksStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "spotify", GroupLabel: "Spotify",
				Explanation: "List one bounded page of tracks from the requested Spotify playlist.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToListPlaylistTracksInput,
			Listed: sdkgo.GoTo(completePlaylistTracks{}),
		})),
		dex.DefineStep(completePlaylistTracks{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the playlist request and result page Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{playlistRequestAttribute, playlistPageAttribute}}
}

// GetDexSummary returns the playlist request and loaded page for the Dex Web run list.
//
// dex:field attribute-key:spotify-playlist-tracks-request value-type:json editable:false description:"Spotify playlist page request"
// dex:field attribute-key:spotify-playlist-tracks-page value-type:json editable:false description:"Loaded Spotify playlist track page"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, page, err := playlistInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"spotify-playlist-tracks-request": request,
		"spotify-playlist-tracks-page":    page,
	}}, nil
}

// GetDexDisplay returns the playlist request and loaded page for the Dex Web run detail.
//
// dex:field attribute-key:spotify-playlist-tracks-request value-type:json editable:false description:"Spotify playlist page request"
// dex:field attribute-key:spotify-playlist-tracks-page value-type:json editable:false description:"Tracks, pagination, and Spotify attribution links"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, page, err := playlistInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"spotify-playlist-tracks-request": request,
		"spotify-playlist-tracks-page":    page,
	}}, nil
}

// MapToListPlaylistTracksInput maps the Flow input to the provider Query input.
func (*Flow) MapToListPlaylistTracksInput(input Input) spotify.ListPlaylistTracksInput {
	return spotify.ListPlaylistTracksInput{
		PlaylistID: input.PlaylistID, Market: input.Market, Limit: input.Limit, Offset: input.Offset,
	}
}

func playlistInspection(ctx dex.Context) (Input, spotify.PlaylistTrackPage, error) {
	request, err := optionalAttribute(ctx, playlistRequestAttribute)
	if err != nil {
		return Input{}, spotify.PlaylistTrackPage{}, err
	}
	page, err := optionalAttribute(ctx, playlistPageAttribute)
	if err != nil {
		return Input{}, spotify.PlaylistTrackPage{}, err
	}
	return request, page, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:spotify group-label:"Spotify"
// dex:explanation text:"Record the playlist page request before calling Spotify."
type recordPlaylistRequest struct {
	dex.StepDefaults
}

func (recordPlaylistRequest) GetStepType() string { return recordPlaylistRequestStepType }

// WaitFor skips immediately because Dex Web Start Flow invokes the start Step's WaitFor.
func (recordPlaylistRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordPlaylistRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := playlistRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](listPlaylistTracksStepType), input), nil
}

// dex:group group-id:spotify group-label:"Spotify"
// dex:explanation text:"Persist the loaded Spotify track page and complete the Flow."
type completePlaylistTracks struct {
	dex.StepDefaultsNoWaitFor[spotify.ListPlaylistTracksResult]
}

func (completePlaylistTracks) GetStepType() string { return completePlaylistTracksStepType }

func (completePlaylistTracks) Execute(ctx dex.Context, result spotify.ListPlaylistTracksResult) (*dex.StepDecision, error) {
	if err := playlistPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
