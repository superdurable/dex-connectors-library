# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListPlaylistTracksInput, PlaylistAlbum, PlaylistArtist, PlaylistTrack, PlaylistTrackPage
from .definitions import CONNECTOR_ID, ACCESS_DENIED, AUTHORIZATION_REVOKED, DEFECT, INVALID_RESPONSE, LISTED, LIST_PLAYLIST_TRACKS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_playlist_tracks_listed(value: PlaylistTrackPage, failure: Failure | None = None) -> Case[PlaylistTrackPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_playlist_tracks_listed, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_authorization_revoked(value: PlaylistTrackPage, failure: Failure | None = None) -> Case[PlaylistTrackPage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, value, failure)

setattr(list_playlist_tracks_authorization_revoked, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_access_denied(value: PlaylistTrackPage, failure: Failure | None = None) -> Case[PlaylistTrackPage]:
    return Case.branch_result(ACCESS_DENIED, value, failure)

setattr(list_playlist_tracks_access_denied, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_not_found(value: PlaylistTrackPage, failure: Failure | None = None) -> Case[PlaylistTrackPage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_playlist_tracks_not_found, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_provider_rejected(value: PlaylistTrackPage, failure: Failure | None = None) -> Case[PlaylistTrackPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_playlist_tracks_provider_rejected, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_invalid_response(value: PlaylistTrackPage, failure: Failure | None = None) -> Case[PlaylistTrackPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_playlist_tracks_invalid_response, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_defect(value: PlaylistTrackPage, failure: Failure | None = None) -> Case[PlaylistTrackPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_playlist_tracks_defect, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_retry(failure: Failure, after_seconds: float = 0) -> Case[PlaylistTrackPage]:
    return Case.retry(failure, after_seconds)

setattr(list_playlist_tracks_retry, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_one_available_track() -> Case[PlaylistTrackPage]:
    return Case.paged(LISTED, (PlaylistTrackPage(playlist_id='3cEYpjA9oz9GiPac4AsH4n', tracks=(PlaylistTrack(position=0, id='4iV5W9uYEdYUVa79Axb7Rh', uri='spotify:track:4iV5W9uYEdYUVa79Axb7Rh', spotify_url='https://open.spotify.com/track/4iV5W9uYEdYUVa79Axb7Rh', name='A Song', artists=(PlaylistArtist(id='0OdUWJ0sBjDrqHygGUXeCF', name='An Artist'),), album=PlaylistAlbum(id='1ATL5GLyefJaxhQzSPVrLX', name='An Album', artists=(PlaylistArtist(id='0OdUWJ0sBjDrqHygGUXeCF', name='An Artist'),)), duration_milliseconds=213000, explicit=False, disc_number=1, track_number=2),), skipped_items=1, limit=2, offset=0, total=5, next_offset=2), PlaylistTrackPage(playlist_id='3cEYpjA9oz9GiPac4AsH4n', tracks=(PlaylistTrack(position=2, id='7ouMYWpwJ422jRcDASZB7P', uri='spotify:track:7ouMYWpwJ422jRcDASZB7P', spotify_url='https://open.spotify.com/track/7ouMYWpwJ422jRcDASZB7P', name='Another Song', artists=(PlaylistArtist(id='0OdUWJ0sBjDrqHygGUXeCF', name='An Artist'),), album=PlaylistAlbum(id='1ATL5GLyefJaxhQzSPVrLX', name='An Album', artists=(PlaylistArtist(id='0OdUWJ0sBjDrqHygGUXeCF', name='An Artist'),)), duration_milliseconds=213000, explicit=False, disc_number=1, track_number=3),), skipped_items=1, limit=2, offset=2, total=5, next_offset=4), PlaylistTrackPage(playlist_id='3cEYpjA9oz9GiPac4AsH4n', tracks=(PlaylistTrack(position=4, id='3n3Ppam7vgaVa1iaRUc9Lp', uri='spotify:track:3n3Ppam7vgaVa1iaRUc9Lp', spotify_url='https://open.spotify.com/track/3n3Ppam7vgaVa1iaRUc9Lp', name='Final Song', artists=(PlaylistArtist(id='0OdUWJ0sBjDrqHygGUXeCF', name='An Artist'),), album=PlaylistAlbum(id='1ATL5GLyefJaxhQzSPVrLX', name='An Album', artists=(PlaylistArtist(id='0OdUWJ0sBjDrqHygGUXeCF', name='An Artist'),)), duration_milliseconds=213000, explicit=False, disc_number=1, track_number=4),), skipped_items=0, limit=2, offset=4, total=5)))

setattr(list_playlist_tracks_one_available_track, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_revoked_grant() -> Case[PlaylistTrackPage]:
    return Case.branch_result(AUTHORIZATION_REVOKED, PlaylistTrackPage(), Failure(FailureKind.AUTHENTICATION, CONNECTOR_ID, "listPlaylistTracks", "Spotify revoked the authorization grant."))

setattr(list_playlist_tracks_revoked_grant, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_inaccessible_playlist() -> Case[PlaylistTrackPage]:
    return Case.branch_result(ACCESS_DENIED, PlaylistTrackPage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listPlaylistTracks", "The user cannot access this playlist."))

setattr(list_playlist_tracks_inaccessible_playlist, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_missing_playlist() -> Case[PlaylistTrackPage]:
    return Case.branch_result(NOT_FOUND, PlaylistTrackPage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listPlaylistTracks", "Spotify could not find the playlist."))

setattr(list_playlist_tracks_missing_playlist, '__module__', "dex_connectors.spotify.spotifymock")

def list_playlist_tracks_invalid_playlist_request() -> Case[PlaylistTrackPage]:
    return Case.branch_result(PROVIDER_REJECTED, PlaylistTrackPage(), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "listPlaylistTracks", "Spotify rejected the playlist query."))

setattr(list_playlist_tracks_invalid_playlist_request, '__module__', "dex_connectors.spotify.spotifymock")


class Mock:

    def __init__(self, connection_name: str = "spotify") -> None:
        self.list_playlist_tracks_script: Query[ListPlaylistTracksInput, PlaylistTrackPage] = Query(LIST_PLAYLIST_TRACKS_DEFINITION)
        self.list_playlist_tracks_script.default(list_playlist_tracks_one_available_track())
        self._connection = new_connection_with_operations(
            list_playlist_tracks=self.list_playlist_tracks_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.spotify.spotifymock")

__all__ = [
    "Mock",
    "list_playlist_tracks_listed",
    "list_playlist_tracks_authorization_revoked",
    "list_playlist_tracks_access_denied",
    "list_playlist_tracks_not_found",
    "list_playlist_tracks_provider_rejected",
    "list_playlist_tracks_invalid_response",
    "list_playlist_tracks_defect",
    "list_playlist_tracks_retry",
    "list_playlist_tracks_one_available_track",
    "list_playlist_tracks_revoked_grant",
    "list_playlist_tracks_inaccessible_playlist",
    "list_playlist_tracks_missing_playlist",
    "list_playlist_tracks_invalid_playlist_request",
]
