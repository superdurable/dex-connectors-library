// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package playlisttracks demonstrates the Spotify listPlaylistTracks Query in a Flow
// started from Dex Web Start Flow.
package playlisttracks

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/spotify/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "SpotifyPlaylistTracks"
	// ConnectionName is the static Dex Web connection that holds Spotify authorization.
	ConnectionName = "spotify-playlist-reader"

	listPlaylistTracksStepType     = "ListPlaylistTracks"
	getPlaylistStepType            = "GetPlaylist"
	continueAfterGetPlaylistType   = "ContinueAfterGetPlaylist"
	getAlbumStepType               = "GetAlbum"
	completeAlbumMetadataStepType  = "CompleteAlbumMetadata"
	getArtistStepType              = "GetArtist"
	completeArtistMetadataStepType = "CompleteArtistMetadata"
	recordPlaylistRequestStepType  = "RecordPlaylistRequest"
	completePlaylistTracksStepType = "CompletePlaylistTracks"
)

var (
	playlistRequestAttribute = dex.DefineAttribute[Input]("spotify-playlist-tracks-request")
	playlistPageAttribute    = dex.DefineAttribute[spotify.PlaylistTrackPage]("spotify-playlist-tracks-page")
	playlistSummaryAttribute = dex.DefineAttribute[spotify.PlaylistSummary]("spotify-playlist-summary")
	albumSummaryAttribute    = dex.DefineAttribute[spotify.AlbumSummary]("spotify-playlist-track-album")
	artistSummaryAttribute   = dex.DefineAttribute[spotify.ArtistSummary]("spotify-playlist-track-artist")
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
		dex.DefineStep(spotify.NewGetPlaylistStep(spotify.GetPlaylistStepConfig[Input]{
			StepType: getPlaylistStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "spotify", GroupLabel: "Spotify",
				Explanation: "Read the playlist owner and snapshot before loading its tracks.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetPlaylistInput,
			Found: sdkgo.GoTo(continueAfterGetPlaylist{}),
		})),
		dex.DefineStep(continueAfterGetPlaylist{}),
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
		dex.DefineStep(spotify.NewGetAlbumStep(spotify.GetAlbumStepConfig[string]{
			StepType: getAlbumStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "spotify", GroupLabel: "Spotify",
				Explanation: "Read the first playlist track's album metadata.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetAlbumInput,
			Found: sdkgo.GoTo(completeAlbumMetadata{}),
		})),
		dex.DefineStep(completeAlbumMetadata{}),
		dex.DefineStep(spotify.NewGetArtistStep(spotify.GetArtistStepConfig[string]{
			StepType: getArtistStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "spotify", GroupLabel: "Spotify",
				Explanation: "Read the first album artist's metadata.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetArtistInput,
			Found: sdkgo.GoTo(completeArtistMetadata{}),
		})),
		dex.DefineStep(completeArtistMetadata{}),
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{playlistRequestAttribute, playlistSummaryAttribute, playlistPageAttribute, albumSummaryAttribute, artistSummaryAttribute}}
}

// GetDexSummary returns the playlist request and loaded page for the Dex Web run list.
//
// dex:field attribute-key:spotify-playlist-tracks-request value-type:json editable:false description:"Spotify playlist page request"
// dex:field attribute-key:spotify-playlist-tracks-page value-type:json editable:false description:"Loaded Spotify playlist track page"
// dex:field attribute-key:spotify-playlist-summary value-type:json editable:false description:"Spotify playlist identity and snapshot"
// dex:field attribute-key:spotify-playlist-track-album value-type:json editable:false description:"First playlist track's album metadata"
// dex:field attribute-key:spotify-playlist-track-artist value-type:json editable:false description:"First album artist's metadata"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, summary, page, album, artist, err := playlistInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"spotify-playlist-tracks-request": request,
		"spotify-playlist-tracks-page":    page,
		"spotify-playlist-summary":        summary,
		"spotify-playlist-track-album":    album,
		"spotify-playlist-track-artist":   artist,
	}}, nil
}

// GetDexDisplay returns the playlist request and loaded page for the Dex Web run detail.
//
// dex:field attribute-key:spotify-playlist-tracks-request value-type:json editable:false description:"Spotify playlist page request"
// dex:field attribute-key:spotify-playlist-tracks-page value-type:json editable:false description:"Tracks, pagination, and Spotify attribution links"
// dex:field attribute-key:spotify-playlist-summary value-type:json editable:false description:"Spotify playlist identity and snapshot"
// dex:field attribute-key:spotify-playlist-track-album value-type:json editable:false description:"First playlist track's album metadata"
// dex:field attribute-key:spotify-playlist-track-artist value-type:json editable:false description:"First album artist's metadata"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, summary, page, album, artist, err := playlistInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"spotify-playlist-tracks-request": request,
		"spotify-playlist-tracks-page":    page,
		"spotify-playlist-summary":        summary,
		"spotify-playlist-track-album":    album,
		"spotify-playlist-track-artist":   artist,
	}}, nil
}

// MapToListPlaylistTracksInput maps the Flow input to the provider Query input.
func (*Flow) MapToListPlaylistTracksInput(input Input) spotify.ListPlaylistTracksInput {
	return spotify.ListPlaylistTracksInput{
		PlaylistID: input.PlaylistID, Market: input.Market, Limit: input.Limit, Offset: input.Offset,
	}
}

// MapToGetPlaylistInput maps the Flow input to the playlist metadata Query.
func (*Flow) MapToGetPlaylistInput(input Input) spotify.GetPlaylistInput {
	return spotify.GetPlaylistInput{PlaylistID: input.PlaylistID}
}

// MapToGetAlbumInput maps an album ID from a playlist track to the album Query.
func (*Flow) MapToGetAlbumInput(albumID string) spotify.GetAlbumInput {
	return spotify.GetAlbumInput{AlbumID: albumID}
}

// MapToGetArtistInput maps an artist ID from the album to the artist Query.
func (*Flow) MapToGetArtistInput(artistID string) spotify.GetArtistInput {
	return spotify.GetArtistInput{ArtistID: artistID}
}

func playlistInspection(ctx dex.Context) (Input, spotify.PlaylistSummary, spotify.PlaylistTrackPage, spotify.AlbumSummary, spotify.ArtistSummary, error) {
	request, err := optionalAttribute(ctx, playlistRequestAttribute)
	if err != nil {
		return Input{}, spotify.PlaylistSummary{}, spotify.PlaylistTrackPage{}, spotify.AlbumSummary{}, spotify.ArtistSummary{}, err
	}
	summary, err := optionalAttribute(ctx, playlistSummaryAttribute)
	if err != nil {
		return Input{}, spotify.PlaylistSummary{}, spotify.PlaylistTrackPage{}, spotify.AlbumSummary{}, spotify.ArtistSummary{}, err
	}
	page, err := optionalAttribute(ctx, playlistPageAttribute)
	if err != nil {
		return Input{}, spotify.PlaylistSummary{}, spotify.PlaylistTrackPage{}, spotify.AlbumSummary{}, spotify.ArtistSummary{}, err
	}
	album, err := optionalAttribute(ctx, albumSummaryAttribute)
	if err != nil {
		return Input{}, spotify.PlaylistSummary{}, spotify.PlaylistTrackPage{}, spotify.AlbumSummary{}, spotify.ArtistSummary{}, err
	}
	artist, err := optionalAttribute(ctx, artistSummaryAttribute)
	if err != nil {
		return Input{}, spotify.PlaylistSummary{}, spotify.PlaylistTrackPage{}, spotify.AlbumSummary{}, spotify.ArtistSummary{}, err
	}
	return request, summary, page, album, artist, nil
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
	return dex.GoTo(sdkgo.StepRef[Input](getPlaylistStepType), input), nil
}

// dex:group group-id:spotify group-label:"Spotify"
// dex:explanation text:"Persist the Spotify playlist metadata and load its tracks."
type continueAfterGetPlaylist struct {
	dex.StepDefaultsNoWaitFor[spotify.GetPlaylistResult]
}

func (continueAfterGetPlaylist) GetStepType() string { return continueAfterGetPlaylistType }

func (continueAfterGetPlaylist) Execute(ctx dex.Context, result spotify.GetPlaylistResult) (*dex.StepDecision, error) {
	if err := playlistSummaryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	request, err := playlistRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](listPlaylistTracksStepType), request), nil
}

// dex:group group-id:spotify group-label:"Spotify"
// dex:explanation text:"Persist the loaded Spotify track page and read its first album when available."
type completePlaylistTracks struct {
	dex.StepDefaultsNoWaitFor[spotify.ListPlaylistTracksResult]
}

func (completePlaylistTracks) GetStepType() string { return completePlaylistTracksStepType }

func (completePlaylistTracks) Execute(ctx dex.Context, result spotify.ListPlaylistTracksResult) (*dex.StepDecision, error) {
	if err := playlistPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	if len(result.Value.Tracks) > 0 && result.Value.Tracks[0].Album.ID != "" {
		return dex.GoTo(sdkgo.StepRef[string](getAlbumStepType), result.Value.Tracks[0].Album.ID), nil
	}
	return dex.GracefulComplete(result.Value), nil
}

// dex:group group-id:spotify group-label:"Spotify"
// dex:explanation text:"Persist the first track's album metadata and read its first artist."
type completeAlbumMetadata struct {
	dex.StepDefaultsNoWaitFor[spotify.GetAlbumResult]
}

func (completeAlbumMetadata) GetStepType() string { return completeAlbumMetadataStepType }

func (completeAlbumMetadata) Execute(ctx dex.Context, result spotify.GetAlbumResult) (*dex.StepDecision, error) {
	if err := albumSummaryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	if len(result.Value.Artists) > 0 && result.Value.Artists[0].ID != "" {
		return dex.GoTo(sdkgo.StepRef[string](getArtistStepType), result.Value.Artists[0].ID), nil
	}
	page, err := playlistPageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(page), nil
}

// dex:group group-id:spotify group-label:"Spotify"
// dex:explanation text:"Persist the first album artist's metadata and complete the playlist page Flow."
type completeArtistMetadata struct {
	dex.StepDefaultsNoWaitFor[spotify.GetArtistResult]
}

func (completeArtistMetadata) GetStepType() string { return completeArtistMetadataStepType }

func (completeArtistMetadata) Execute(ctx dex.Context, result spotify.GetArtistResult) (*dex.StepDecision, error) {
	if err := artistSummaryAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	page, err := playlistPageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(page), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
