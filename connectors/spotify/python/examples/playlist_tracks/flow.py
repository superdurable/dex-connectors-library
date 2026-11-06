# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Spotify playlist Query Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.spotify import (
    Connection,
    ListPlaylistTracksInput,
    PlaylistTrackPage,
    new_list_playlist_tracks_step,
)

CONNECTION_NAME = "spotify-playlist-reader"


@dataclass(frozen=True, slots=True)
class PlaylistInput:
    """Playlist and page requested by the application user."""

    playlist_id: str = field(metadata={"dex_json_name": "playlistId"})
    market: str = ""
    limit: int = 2
    offset: int = 0

    def to_operation_input(self) -> ListPlaylistTracksInput:
        """Map safe Flow input to the connector operation."""
        return ListPlaylistTracksInput(self.playlist_id, self.market, self.limit, self.offset)


playlist_page = dex.Attribute("spotify-playlist-page", PlaylistTrackPage)


# dex:group group-id:spotify-playlist group-label:"Spotify Playlist"
# dex:explanation text:"Persist the bounded Spotify playlist page and complete the Flow."
class CompletePlaylist(dex.Step[QueryResult[PlaylistTrackPage]]):
    """Persist the listed page for Dex Web and return it."""

    def execute(self, context: dex.Context, input: QueryResult[PlaylistTrackPage]) -> dex.StepDecision:
        """Complete after storing only public track metadata."""
        playlist_page.set(context, input.value)
        return dex.graceful_complete(input.value)


class PlaylistFlow(dex.Flow[PlaylistInput]):
    """List one page through a static Spotify connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the provider Query before Worker registration."""
        self.complete = CompletePlaylist()
        self.list_tracks = new_list_playlist_tracks_step(
            step_type="ListPlaylistTracks",
            step_input_type=PlaylistInput,
            annotations={
                "groupId": "spotify-playlist",
                "groupLabel": "Spotify Playlist",
                "explanation": "List one bounded page of playlist tracks.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=PlaylistInput.to_operation_input,
            listed=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the example Flow identity stable."""
        return "SpotifyPlaylistTracksPython"

    def get_steps(self) -> dex.StepList[PlaylistInput]:
        """Query Spotify and store the successful page."""
        return dex.StepList.start_step(self.list_tracks).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the page Attribute used by the completion Step."""
        return dex.PersistenceSchema.of(playlist_page)
