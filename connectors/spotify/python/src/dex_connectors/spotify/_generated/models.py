# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListPlaylistTracksInput selects one bounded page of Spotify playlist tracks.
@dataclass(frozen=True, slots=True)
class ListPlaylistTracksInput:
    # PlaylistID is the 22-character Spotify playlist identifier.
    playlist_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'playlistId'})
    # Market is an optional ISO 3166-1 alpha-2 country code. Spotify prefers the authorized user's country.
    market: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Limit is 1 through 50. Zero selects Spotify's default page size of 20.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Offset is the zero-based playlist item offset.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})


setattr(ListPlaylistTracksInput, '__module__', 'dex_connectors.spotify.models')


# PlaylistArtist is bounded Spotify artist metadata with an attribution link.
@dataclass(frozen=True, slots=True)
class PlaylistArtist:
    # ID is the Spotify artist ID when available.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URI is the Spotify artist URI when available.
    uri: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SpotifyURL links back to this artist on Spotify when available.
    spotify_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'spotifyUrl', 'dex_json_omit_default': True})
    # Name is Spotify's unmodified artist name.
    name: str = ''


setattr(PlaylistArtist, '__module__', 'dex_connectors.spotify.models')


# PlaylistAlbum is bounded Spotify album metadata with an attribution link.
@dataclass(frozen=True, slots=True)
class PlaylistAlbum:
    # ID is the Spotify album ID when available.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URI is the Spotify album URI when available.
    uri: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SpotifyURL links back to this album on Spotify when available.
    spotify_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'spotifyUrl', 'dex_json_omit_default': True})
    # Name is Spotify's unmodified album name.
    name: str = ''
    # ReleaseDate is Spotify's year, year-month, or full release date.
    release_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'releaseDate', 'dex_json_omit_default': True})
    # ReleaseDatePrecision is year, month, or day when Spotify supplies a release date.
    release_date_precision: str = _dataclass_field(default='', metadata={'dex_json_name': 'releaseDatePrecision', 'dex_json_omit_default': True})
    # Artists are Spotify's album artists in provider order.
    artists: tuple[PlaylistArtist, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'artists', _replace_nil_slices(self.artists, 1))


PlaylistAlbum.__annotations__['artists'] = tuple[PlaylistArtist, ...] | None
PlaylistAlbum.__dataclass_fields__['artists'].type = tuple[PlaylistArtist, ...] | None
setattr(PlaylistAlbum, '__module__', 'dex_connectors.spotify.models')


# PlaylistTrack is one available Spotify track and its playlist metadata.
@dataclass(frozen=True, slots=True)
class PlaylistTrack:
    # Position is the track's zero-based position in the playlist page's current snapshot.
    position: int = 0
    # AddedAt is when Spotify reports the item was added. Old playlist items may omit it.
    added_at: datetime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'addedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # AddedByUserID is the Spotify user ID that added the item, when available.
    added_by_user_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'addedByUserId', 'dex_json_omit_default': True})
    # IsLocal reports that the item refers to a local file instead of Spotify-hosted content.
    is_local: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isLocal'})
    # ID is the Spotify track ID. Local files may omit it.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URI is Spotify's track or local-file URI.
    uri: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SpotifyURL links back to this track on Spotify when one is available.
    spotify_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'spotifyUrl', 'dex_json_omit_default': True})
    # Name is Spotify's unmodified track name.
    name: str = ''
    # Artists are Spotify's track artists in provider order.
    artists: tuple[PlaylistArtist, ...] = ()
    # Album is the Spotify album on which the track appears.
    album: PlaylistAlbum = _dataclass_field(default_factory=PlaylistAlbum)
    # DurationMilliseconds is Spotify's track duration in milliseconds.
    duration_milliseconds: int = _dataclass_field(default=0, metadata={'dex_json_name': 'durationMilliseconds'})
    # Explicit reports Spotify's explicit-content classification.
    explicit: bool = False
    # IsPlayable is Spotify's market-specific playability when the API includes it.
    is_playable: bool | None = _dataclass_field(default=None, metadata={'dex_json_name': 'isPlayable', 'dex_json_omit_default': True})
    # DiscNumber is the track's disc number within the album.
    disc_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'discNumber'})
    # TrackNumber is the track's number on its disc.
    track_number: int = _dataclass_field(default=0, metadata={'dex_json_name': 'trackNumber'})

    def __post_init__(self) -> None:
        if isinstance(self.added_at, str):
            object.__setattr__(self, 'added_at', parse_go_datetime(self.added_at, 'added_at'))
        elif self.added_at is not None:
            validate_go_datetime(self.added_at, 'added_at')
        object.__setattr__(self, 'artists', _replace_nil_slices(self.artists, 1))


PlaylistTrack.__annotations__['artists'] = tuple[PlaylistArtist, ...] | None
PlaylistTrack.__dataclass_fields__['artists'].type = tuple[PlaylistArtist, ...] | None
setattr(PlaylistTrack, '__module__', 'dex_connectors.spotify.models')


# PlaylistTrackPage is one bounded page of playlist tracks in Spotify order.
@dataclass(frozen=True, slots=True)
class PlaylistTrackPage:
    # PlaylistID is the requested Spotify playlist identifier.
    playlist_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'playlistId'})
    # Tracks contains available track items in playlist order.
    tracks: tuple[PlaylistTrack, ...] = ()
    # SkippedItems counts unavailable or non-track items omitted from Tracks.
    skipped_items: int = _dataclass_field(default=0, metadata={'dex_json_name': 'skippedItems'})
    # Limit is Spotify's page-size value for this response.
    limit: int = 0
    # Offset is Spotify's zero-based offset for this response.
    offset: int = 0
    # Total is Spotify's current playlist item count.
    total: int = 0
    # NextOffset is the next page's zero-based offset, or zero when no page follows.
    next_offset: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextOffset', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'tracks', _replace_nil_slices(self.tracks, 1))


PlaylistTrackPage.__annotations__['tracks'] = tuple[PlaylistTrack, ...] | None
PlaylistTrackPage.__dataclass_fields__['tracks'].type = tuple[PlaylistTrack, ...] | None
setattr(PlaylistTrackPage, '__module__', 'dex_connectors.spotify.models')
