# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Search one YouTube page through a static Dex connection."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.google_youtube import (
    Connection,
    SearchVideosInput,
    VideoSearchPage,
    new_search_videos_step,
)

CONNECTION_NAME = "youtube-playlist-curator"


@dataclass(frozen=True, slots=True)
class SearchInput:
    """Keyword and channel for one bounded search page."""

    query: str
    channel_id: str = field(default="", metadata={"dex_json_name": "channelId"})

    def to_operation_input(self) -> SearchVideosInput:
        """Map Flow input to the YouTube Query."""
        return SearchVideosInput(query=self.query, channel_id=self.channel_id, max_results=5)


search_page = dex.Attribute("youtube-python-search-page", VideoSearchPage)


# dex:group group-id:youtube-search group-label:"YouTube Search"
# dex:explanation text:"Persist the public search page and complete the Flow."
class CompleteSearch(dex.Step[QueryResult[VideoSearchPage]]):
    """Persist public video metadata and complete."""

    def execute(self, context: dex.Context, input: QueryResult[VideoSearchPage]) -> dex.StepDecision:
        """Store the page for Dex Web and return it."""
        search_page.set(context, input.value)
        return dex.graceful_complete(input.value)


class VideoSearchFlow(dex.Flow[SearchInput]):
    """Search YouTube using a named project connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the generated search Step before registration."""
        self.complete = CompleteSearch()
        self.search = new_search_videos_step(
            step_type="SearchVideos",
            step_input_type=SearchInput,
            annotations={
                "groupId": "youtube-search",
                "groupLabel": "YouTube Search",
                "explanation": "Search one bounded page of YouTube videos.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=SearchInput.to_operation_input,
            searched=self.complete,
        )

    def get_flow_type(self) -> str:
        """Keep the runnable Flow identity stable."""
        return "YouTubeVideoSearchPython"

    def get_steps(self) -> dex.StepList[SearchInput]:
        """Start at the search Query and finish on its happy branch."""
        return dex.StepList.start_step(self.search).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the page Attribute consumed by the completion Step."""
        return dex.PersistenceSchema.of(search_page)
