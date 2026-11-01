# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Unsplash scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, FOUND, GET_PHOTO_DEFINITION, NOT_FOUND, SEARCH_PHOTOS_DEFINITION, SEARCHED
from ..connection import Connection, new_connection_with_operations
from ..models import GetPhotoInput, Photo, PhotoPage, SearchPhotosInput

_OFFICE = Photo(
    id="office-1",
    html_url="https://unsplash.com/photos/office-1",
    regular_url="https://images.unsplash.com/photo-office-1",
    photographer_name="A. Photographer",
    photographer_url="https://unsplash.com/@photographer",
    description="A sunlit office",
    alt_description="sunlit office interior",
)


def search_photos_searched(value: PhotoPage, failure: Failure | None = None) -> Case[PhotoPage]:
    """Script the searched route."""
    return Case.branch_result(SEARCHED, value, failure)


def search_photos_default_answer() -> Case[PhotoPage]:
    """Return the manifest's searchPhotos default answer."""
    return search_photos_searched(PhotoPage(1, 1, (_OFFICE,)))


def get_photo_found(value: Photo, failure: Failure | None = None) -> Case[Photo]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_photo_not_found(value: Photo, failure: Failure | None = None) -> Case[Photo]:
    """Script a missing photo."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_photo_default_answer() -> Case[Photo]:
    """Return the manifest's getPhoto default answer."""
    return get_photo_found(_OFFICE)


class Mock:
    """Script Unsplash Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "unsplash") -> None:
        self.search_photos_script: Query[SearchPhotosInput, PhotoPage] = Query(SEARCH_PHOTOS_DEFINITION)
        self.search_photos_script.default(search_photos_default_answer())
        self.get_photo_script: Query[GetPhotoInput, Photo] = Query(GET_PHOTO_DEFINITION)
        self.get_photo_script.default(get_photo_default_answer())
        self._connection = new_connection_with_operations(
            self.search_photos_script, self.get_photo_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "search_photos_searched",
    "search_photos_default_answer",
    "get_photo_found",
    "get_photo_not_found",
    "get_photo_default_answer",
]
