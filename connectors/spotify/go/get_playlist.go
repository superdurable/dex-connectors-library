// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spotify

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetPlaylistInput identifies a playlist visible to the authorized user.
type GetPlaylistInput struct {
	// PlaylistID is the 22-character Spotify playlist identifier from a Spotify playlist URL.
	PlaylistID string `json:"playlistId"`
}

// PlaylistSummary contains bounded metadata and a link back to Spotify.
type PlaylistSummary struct {
	// ID is the stable Spotify playlist identifier.
	ID string `json:"id"`
	// Name is the playlist name.
	Name string `json:"name"`
	// Description is the playlist owner's text, when available.
	Description string `json:"description,omitempty"`
	// OwnerUserID identifies the playlist owner.
	OwnerUserID string `json:"ownerUserId"`
	// SpotifyURL links to the playlist on Spotify.
	SpotifyURL string `json:"spotifyUrl"`
	// SnapshotID identifies the playlist version at the time of this read.
	SnapshotID string `json:"snapshotId"`
	// Public indicates profile visibility; nil means Spotify did not specify it.
	Public *bool `json:"public,omitempty"`
}

// GetPlaylistOperation reads the metadata of one playlist.
type GetPlaylistOperation struct{ client *Client }

type spotifyPlaylistSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Owner       struct {
		ID string `json:"id"`
	} `json:"owner"`
	ExternalURLs spotifyExternalURLs `json:"external_urls"`
	SnapshotID   string              `json:"snapshot_id"`
	Public       *bool               `json:"public"`
}

// GetPlaylist returns the GetPlaylist Query bound to this client.
func (client *Client) GetPlaylist() GetPlaylistOperation { return GetPlaylistOperation{client: client} }

// Definition returns the immutable connector operation definition.
func (GetPlaylistOperation) Definition() sdkgo.QueryDefinition { return GetPlaylistDefinition }

// Invoke reads one playlist, classifying provider failures without exposing response text.
func (operation GetPlaylistOperation) Invoke(call sdkgo.Call, input GetPlaylistInput) sdkgo.QueryAttempt[PlaylistSummary] {
	if !spotifyIDPattern.MatchString(input.PlaylistID) {
		return playlistSummaryFailure(GetPlaylistBranchDefect, sdkgo.FailureValidation, "playlistId must be a 22-character Spotify ID")
	}
	credential, failure := operation.client.resolveCredential(call)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetPlaylistBranchDefect, PlaylistSummary{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.getPlaylistDetails(call, &credential, input.PlaylistID)
	if errors.Is(err, errResponseTooLarge) {
		return playlistSummaryFailure(GetPlaylistBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Spotify playlist response exceeds the configured size limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[PlaylistSummary](spotifyFailure(sdkgo.FailureAvailability, "Spotify is unavailable"), 0)
	}
	switch response.statusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return playlistSummaryFailure(GetPlaylistBranchAuthorizationRevoked, sdkgo.FailureAuthentication, "Spotify authorization is invalid or revoked")
	case http.StatusForbidden:
		return playlistSummaryFailure(GetPlaylistBranchAccessDenied, sdkgo.FailureAuthorization, "Spotify denied access to the playlist")
	case http.StatusNotFound:
		return playlistSummaryFailure(GetPlaylistBranchNotFound, sdkgo.FailureNotFound, "Spotify playlist was not found")
	case http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[PlaylistSummary](spotifyFailure(sdkgo.FailureRateLimit, "Spotify rate limit was reached"), retryDelay(response.header))
	default:
		if response.statusCode >= 500 {
			return sdkgo.NewQueryRetry[PlaylistSummary](spotifyFailure(sdkgo.FailureAvailability, "Spotify is unavailable"), 0)
		}
		return playlistSummaryFailure(GetPlaylistBranchProviderRejected, sdkgo.FailureProviderRejection, "Spotify rejected the playlist query")
	}
	var providerPlaylist spotifyPlaylistSummary
	if err := decodeJSON(response.body, &providerPlaylist); err != nil || providerPlaylist.ID != input.PlaylistID {
		return playlistSummaryFailure(GetPlaylistBranchInvalidResponse, sdkgo.FailureProtocol, "Spotify returned invalid playlist metadata")
	}
	summary, err := convertPlaylistSummary(providerPlaylist)
	if err != nil {
		return playlistSummaryFailure(GetPlaylistBranchInvalidResponse, sdkgo.FailureProtocol, "Spotify returned invalid playlist metadata")
	}
	return sdkgo.NewQueryBranch(GetPlaylistBranchFound, summary, nil, sdkgo.Receipt{})
}

func convertPlaylistSummary(providerPlaylist spotifyPlaylistSummary) (PlaylistSummary, error) {
	if !spotifyIDPattern.MatchString(providerPlaylist.ID) ||
		validateMetadataString(providerPlaylist.Name, "playlist name", true) != nil ||
		validateMetadataString(providerPlaylist.Description, "playlist description", false) != nil ||
		validateMetadataString(providerPlaylist.Owner.ID, "playlist owner", true) != nil ||
		validateMetadataString(providerPlaylist.SnapshotID, "playlist snapshot", true) != nil {
		return PlaylistSummary{}, errors.New("invalid Spotify playlist metadata")
	}
	spotifyLink, err := spotifyURL(providerPlaylist.ExternalURLs.Spotify)
	if err != nil {
		return PlaylistSummary{}, err
	}
	return PlaylistSummary{
		ID: providerPlaylist.ID, Name: providerPlaylist.Name, Description: providerPlaylist.Description,
		OwnerUserID: providerPlaylist.Owner.ID, SpotifyURL: spotifyLink,
		SnapshotID: providerPlaylist.SnapshotID, Public: providerPlaylist.Public,
	}, nil
}

func (client *Client) getPlaylistDetails(call sdkgo.Call, credential *Credentials, playlistID string) (providerResponse, error) {
	return client.getSpotifyRead(call, credential, "/playlists/"+url.PathEscape(playlistID), nil)
}

func (client *Client) getSpotifyRead(call sdkgo.Call, credential *Credentials, path string, query url.Values) (providerResponse, error) {
	requestURL := strings.TrimRight(client.endpoint.String(), "/") + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	for attempt := 0; attempt < 2; attempt++ {
		request, err := http.NewRequestWithContext(call.Context, http.MethodGet, requestURL, nil)
		if err != nil {
			return providerResponse{}, err
		}
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
		request.Header.Set("User-Agent", userAgent)
		response, err := client.httpClient.Do(request)
		if err != nil {
			return providerResponse{}, err
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil {
			return providerResponse{}, errors.Join(readErr, closeErr)
		}
		result := providerResponse{statusCode: response.StatusCode, header: response.Header.Clone(), body: body}
		if int64(len(body)) > client.maxResponseBytes {
			return providerResponse{}, errResponseTooLarge
		}
		if response.StatusCode != http.StatusUnauthorized || attempt != 0 {
			return result, nil
		}
		if _, ok := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials]); !ok {
			return result, nil
		}
		replacement, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		if err != nil || validateResolvedCredentials(replacement) != nil {
			return result, nil
		}
		*credential = replacement
	}
	return providerResponse{}, errors.New("Spotify authenticated request retry was exhausted")
}

func playlistSummaryFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PlaylistSummary] {
	failure := spotifyFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, PlaylistSummary{}, &failure, sdkgo.Receipt{})
}
