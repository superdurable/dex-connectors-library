// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package playlistcurator demonstrates the YouTube connector in a Flow started
// from Dex Web: it finds a channel's newest video that matches a query, adds it
// once to a playlist the account owns, and reads the playlist back to confirm.
package playlistcurator

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	youtube "github.com/superdurable/dex-connectors-library/connectors/google/youtube"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity that Dex Web Start Flow sends from the Flow Definition.
	FlowType = "YouTubePlaylistCurator"
	// ReadConnectionName is the Dex Web connection authorized with the read-only method and youtube.readonly scope.
	ReadConnectionName = "youtube-readonly"
	// ManageConnectionName is the Dex Web connection authorized with the manage method and youtube scope; only addPlaylistItem uses it.
	ManageConnectionName = "youtube"

	lookupChannelStepType       = "LookupChannel"
	searchLatestVideoStepType   = "SearchLatestVideo"
	addToPlaylistStepType       = "AddToPlaylist"
	confirmPlaylistItemStepType = "ConfirmPlaylistItem"
	maxQueryCharacters          = 200
)

var (
	curationRequestAttribute = dex.DefineAttribute[CurationRequest]("youtube-curation-request")
	curationOutcomeAttribute = dex.DefineAttribute[CurationOutcome]("youtube-curation-outcome")
)

// CurationRequest is the typed start input entered in Dex Web Start Flow.
type CurationRequest struct {
	// Channel is a channel handle such as @GoogleDevelopers, or a channel ID starting with UC.
	Channel string `json:"channel"`
	// Query is the keyword search within the channel, such as "release notes".
	Query string `json:"query"`
	// PlaylistID is the playlist the authorized account owns, from youtube.com/playlist?list=<ID>.
	PlaylistID string `json:"playlistId"`
	// ChannelID is the resolved channel ID. ChannelResolved sets it.
	ChannelID string `json:"channelId,omitempty"`
}

// Status is the CurationOutcome route that completed the Flow.
type Status string

const (
	// StatusAdded means the playlist holds the newest matching video, confirmed by a read-back.
	StatusAdded Status = "added"
	// StatusNoMatch means the channel has no video matching the query.
	StatusNoMatch Status = "noMatch"
	// StatusChannelNotFound means no channel has the handle or ID.
	StatusChannelNotFound Status = "channelNotFound"
)

// CurationOutcome is the Flow completion output. Titles are untrusted provider text.
type CurationOutcome struct {
	Status            Status `json:"status"`
	ChannelID         string `json:"channelId,omitempty"`
	ChannelTitle      string `json:"channelTitle,omitempty"`
	VideoID           string `json:"videoId,omitempty"`
	VideoTitle        string `json:"videoTitle,omitempty"`
	PlaylistID        string `json:"playlistId"`
	PlaylistItemID    string `json:"playlistItemId,omitempty"`
	WasAlreadyPresent bool   `json:"wasAlreadyPresent"`
	IsConfirmed       bool   `json:"isConfirmed"`
}

// Flow curates one video into one playlist. Reads use the read-only connection,
// so the youtube scope is used only for the one playlist change.
type Flow struct {
	dex.FlowDefaults
	readConnection   youtube.Connection
	manageConnection youtube.Connection
}

// NewFlow binds the trusted read-only and manage Connections at registration time.
func NewFlow(readConnection youtube.Connection, manageConnection youtube.Connection) *Flow {
	return &Flow{readConnection: readConnection, manageConnection: manageConnection}
}

func (*Flow) GetFlowType() string { return FlowType }

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCurationRequest{}),
		dex.DefineStep(youtube.NewGetChannelStep(youtube.GetChannelStepConfig[CurationRequest]{
			StepType: lookupChannelStepType, ConnectionName: ReadConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "youtube-channel", GroupLabel: "YouTube channel",
				Explanation: "Resolve the channel handle or ID to a YouTube channel.",
			},
			Connection: flow.readConnection, MapToOperationInput: MapToGetChannelInput,
			Found: sdkgo.GoTo(channelResolved{}), NotFound: sdkgo.GoTo(channelNotFound{}),
		})),
		dex.DefineStep(channelResolved{}),
		dex.DefineStep(youtube.NewSearchVideosStep(youtube.SearchVideosStepConfig[CurationRequest]{
			StepType: searchLatestVideoStepType, ConnectionName: ReadConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "youtube-search", GroupLabel: "YouTube search",
				Explanation: "Search the channel for the newest video matching the query.",
			},
			Connection: flow.readConnection, MapToOperationInput: MapToSearchVideosInput,
			Searched: sdkgo.GoTo(latestVideoChosen{}),
		})),
		dex.DefineStep(latestVideoChosen{}),
		dex.DefineStep(youtube.NewAddPlaylistItemStep(youtube.AddPlaylistItemStepConfig[youtube.AddPlaylistItemInput]{
			StepType: addToPlaylistStepType, ConnectionName: ManageConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "youtube-playlist", GroupLabel: "YouTube playlist",
				Explanation: "Add the video to the playlist unless the playlist already holds it.",
			},
			Connection: flow.manageConnection, MapToOperationInput: func(input youtube.AddPlaylistItemInput) youtube.AddPlaylistItemInput { return input },
			Added: sdkgo.GoTo(videoAdded{}),
		})),
		dex.DefineStep(videoAdded{}),
		dex.DefineStep(youtube.NewListPlaylistItemsStep(youtube.ListPlaylistItemsStepConfig[youtube.ListPlaylistItemsInput]{
			StepType: confirmPlaylistItemStepType, ConnectionName: ReadConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "youtube-confirm", GroupLabel: "YouTube confirmation",
				Explanation: "Read the playlist back filtered to the video to confirm it holds the item.",
			},
			Connection: flow.readConnection, MapToOperationInput: func(input youtube.ListPlaylistItemsInput) youtube.ListPlaylistItemsInput { return input },
			Listed: sdkgo.GoTo(curationConfirmed{}),
		})),
		dex.DefineStep(curationConfirmed{}),
		dex.DefineStep(channelNotFound{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{curationRequestAttribute, curationOutcomeAttribute}}
}

// dex:field attribute-key:youtube-curation-request value-type:json editable:false description:"Curation request"
// dex:field attribute-key:youtube-curation-outcome value-type:json editable:false description:"Curation outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := curationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"youtube-curation-request": request,
		"youtube-curation-outcome": outcome,
	}}, nil
}

// dex:field attribute-key:youtube-curation-request value-type:json editable:false description:"Channel, query, and target playlist"
// dex:field attribute-key:youtube-curation-outcome value-type:json editable:false description:"Chosen video, playlist item, and read-back confirmation"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := curationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"youtube-curation-request": request,
		"youtube-curation-outcome": outcome,
	}}, nil
}

func curationInspection(ctx dex.Context) (CurationRequest, CurationOutcome, error) {
	request, err := optionalAttribute(ctx, curationRequestAttribute)
	if err != nil {
		return CurationRequest{}, CurationOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, curationOutcomeAttribute)
	if err != nil {
		return CurationRequest{}, CurationOutcome{}, err
	}
	return request, outcome, nil
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

// PrepareCurationRequest trims and validates the start input.
func PrepareCurationRequest(request CurationRequest) (CurationRequest, error) {
	request.Channel = strings.TrimSpace(request.Channel)
	request.Query = strings.TrimSpace(request.Query)
	request.PlaylistID = strings.TrimSpace(request.PlaylistID)
	request.ChannelID = ""
	if request.Channel == "" {
		return CurationRequest{}, errors.New("channel must be a handle such as @GoogleDevelopers or a channel ID")
	}
	if request.Query == "" || utf8.RuneCountInString(request.Query) > maxQueryCharacters {
		return CurationRequest{}, fmt.Errorf("query must be 1 to %d characters", maxQueryCharacters)
	}
	if request.PlaylistID == "" {
		return CurationRequest{}, errors.New("playlistId is required")
	}
	return request, nil
}

// MapToGetChannelInput treats a value starting with UC and 24 characters long as a channel ID, and anything else as a handle.
func MapToGetChannelInput(request CurationRequest) youtube.GetChannelInput {
	if strings.HasPrefix(request.Channel, "UC") && len(request.Channel) == 24 {
		return youtube.GetChannelInput{ChannelID: request.Channel}
	}
	return youtube.GetChannelInput{Handle: request.Channel}
}

// MapToSearchVideosInput searches the resolved channel for the single newest match.
func MapToSearchVideosInput(request CurationRequest) youtube.SearchVideosInput {
	return youtube.SearchVideosInput{Query: request.Query, ChannelID: request.ChannelID, Order: "date", MaxResults: 1}
}

// dex:group group-id:curation-request group-label:"Curation request"
// dex:explanation text:"Validate the channel, query, and playlist, and persist the request before calling YouTube."
type recordCurationRequest struct {
	dex.StepDefaults
}

func (recordCurationRequest) GetStepType() string { return "RecordCurationRequest" }

// WaitFor skips immediately; Dex Web Start Flow invokes the start Step's WaitFor.
func (recordCurationRequest) WaitFor(dex.Context, CurationRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCurationRequest) Execute(ctx dex.Context, request CurationRequest) (*dex.StepDecision, error) {
	prepared, err := PrepareCurationRequest(request)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := curationRequestAttribute.Set(ctx, prepared); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CurationRequest](lookupChannelStepType), prepared), nil
}

// dex:group group-id:curation-channel group-label:"Channel resolution"
// dex:explanation text:"Record the resolved channel ID so the search covers only that channel."
type channelResolved struct {
	dex.StepDefaultsNoWaitFor[youtube.GetChannelResult]
}

func (channelResolved) GetStepType() string { return "ChannelResolved" }

func (channelResolved) Execute(ctx dex.Context, result youtube.GetChannelResult) (*dex.StepDecision, error) {
	request, err := curationRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	request.ChannelID = result.Value.ID
	if err := curationRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	if err := curationOutcomeAttribute.Set(ctx, CurationOutcome{ChannelID: result.Value.ID, ChannelTitle: result.Value.Title, PlaylistID: request.PlaylistID}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CurationRequest](searchLatestVideoStepType), request), nil
}

// dex:group group-id:curation-selection group-label:"Video selection"
// dex:explanation text:"Pick the newest matching video, or complete without changes when nothing matches."
type latestVideoChosen struct {
	dex.StepDefaultsNoWaitFor[youtube.SearchVideosResult]
}

func (latestVideoChosen) GetStepType() string { return "LatestVideoChosen" }

func (latestVideoChosen) Execute(ctx dex.Context, result youtube.SearchVideosResult) (*dex.StepDecision, error) {
	request, err := curationRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := curationOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Results) == 0 {
		outcome.Status = StatusNoMatch
		if err := curationOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	video := result.Value.Results[0]
	outcome.VideoID, outcome.VideoTitle = video.VideoID, video.Title
	if err := curationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[youtube.AddPlaylistItemInput](addToPlaylistStepType), youtube.AddPlaylistItemInput{PlaylistID: request.PlaylistID, VideoID: video.VideoID}), nil
}

// dex:group group-id:curation-added group-label:"Playlist update"
// dex:explanation text:"Record the playlist item and whether the playlist already held the video."
type videoAdded struct {
	dex.StepDefaultsNoWaitFor[youtube.AddPlaylistItemResult]
}

func (videoAdded) GetStepType() string { return "VideoAdded" }

func (videoAdded) Execute(ctx dex.Context, result youtube.AddPlaylistItemResult) (*dex.StepDecision, error) {
	outcome, err := curationOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.PlaylistItemID, outcome.WasAlreadyPresent = result.Value.Item.ID, result.Value.WasAlreadyPresent
	if err := curationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[youtube.ListPlaylistItemsInput](confirmPlaylistItemStepType), youtube.ListPlaylistItemsInput{
		PlaylistID: outcome.PlaylistID, VideoID: outcome.VideoID, MaxResults: 1,
	}), nil
}

// dex:group group-id:curation-outcome group-label:"Curation outcome"
// dex:explanation text:"Complete with the curated video and whether the read-back found it in the playlist."
type curationConfirmed struct {
	dex.StepDefaultsNoWaitFor[youtube.ListPlaylistItemsResult]
}

func (curationConfirmed) GetStepType() string { return "CurationConfirmed" }

func (curationConfirmed) Execute(ctx dex.Context, result youtube.ListPlaylistItemsResult) (*dex.StepDecision, error) {
	outcome, err := curationOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Status = StatusAdded
	outcome.IsConfirmed = len(result.Value.Items) > 0 && result.Value.Items[0].VideoID == outcome.VideoID
	if err := curationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:curation-missing-channel group-label:"Missing channel"
// dex:explanation text:"Complete without changes because no channel has the handle or ID."
type channelNotFound struct {
	dex.StepDefaultsNoWaitFor[youtube.GetChannelResult]
}

func (channelNotFound) GetStepType() string { return "ChannelNotFound" }

func (channelNotFound) Execute(ctx dex.Context, _ youtube.GetChannelResult) (*dex.StepDecision, error) {
	request, err := curationRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := CurationOutcome{Status: StatusChannelNotFound, PlaylistID: request.PlaylistID}
	if err := curationOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
