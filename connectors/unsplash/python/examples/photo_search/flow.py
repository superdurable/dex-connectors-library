# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Unsplash Flow that searches public photos by phrase."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.unsplash import Connection, PhotoPage, SearchPhotosInput, new_search_photos_step

FLOW_TYPE = "UnsplashPhotoSearch"
CONNECTION_NAME = "unsplash"
_SEARCH_STEP_TYPE = "SearchUnsplashPhotos"
_OUTCOME = dex.Attribute("unsplash-photo-search-outcome", PhotoPage)


@dataclass(frozen=True, slots=True)
class PhotoSearchRequest:
    """Search phrase and photos per page; zero means 10, at most 30."""

    query: str
    per_page: int = field(default=0, metadata={"dex_json_name": "perPage", "dex_json_omit_default": True})


def map_to_search_photos_input(request: PhotoSearchRequest) -> SearchPhotosInput:
    """Search the trimmed phrase at the prepared page size."""
    return SearchPhotosInput(query=request.query, per_page=request.per_page)


# dex:group group-id:photo-search group-label:"Photo search"
# dex:explanation text:"Trim the phrase and fix the page size before searching."
class _Start(dex.Step[PhotoSearchRequest]):
    def get_step_type(self) -> str:
        return "StartUnsplashPhotoSearch"

    def execute(self, context: dex.Context, input: PhotoSearchRequest) -> dex.StepDecision:
        query = input.query.strip()
        if not query or not 0 <= input.per_page <= 30:
            return dex.force_fail("query is required and perPage must be 1 through 30")
        return dex.go_to(dex.StepRef(_SEARCH_STEP_TYPE), PhotoSearchRequest(query, input.per_page or 10))


# dex:group group-id:photo-search group-label:"Photo search"
# dex:explanation text:"Record the matching photos and complete the Flow."
class _Searched(dex.Step[QueryResult[PhotoPage]]):
    def get_step_type(self) -> str:
        return "UnsplashPhotosFound"

    def execute(self, context: dex.Context, input: QueryResult[PhotoPage]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class PhotoSearchFlow(dex.Flow[PhotoSearchRequest]):
    """Search Unsplash photos through the static unsplash connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("photo search requires the static unsplash connection")
        self._start = _Start()
        self._searched = _Searched()
        self._search = new_search_photos_step(
            step_type=_SEARCH_STEP_TYPE,
            step_input_type=PhotoSearchRequest,
            annotations={
                "groupId": "photo-search",
                "groupLabel": "Photo search",
                "explanation": "Search Unsplash for public photos matching the phrase.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_search_photos_input,
            searched=self._searched,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[PhotoSearchRequest]:
        """Register the start, photo search, and result Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._search, self._searched)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the photo page."""
        return dex.PersistenceSchema.of(_OUTCOME)
