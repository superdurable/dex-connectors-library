# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPhotoInput, Photo, PhotoPage, SearchPhotosInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PHOTO_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED, SEARCHED, SEARCH_PHOTOS_DEFINITION

def search_photos_searched(value: PhotoPage, failure: Failure | None = None) -> Case[PhotoPage]:
    """Script the searched branch."""
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_photos_searched, '__module__', "dex_connectors.unsplash.unsplashmock")

def search_photos_provider_rejected(value: PhotoPage, failure: Failure | None = None) -> Case[PhotoPage]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_photos_provider_rejected, '__module__', "dex_connectors.unsplash.unsplashmock")

def search_photos_invalid_response(value: PhotoPage, failure: Failure | None = None) -> Case[PhotoPage]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_photos_invalid_response, '__module__', "dex_connectors.unsplash.unsplashmock")

def search_photos_defect(value: PhotoPage, failure: Failure | None = None) -> Case[PhotoPage]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(search_photos_defect, '__module__', "dex_connectors.unsplash.unsplashmock")

def search_photos_retry(failure: Failure, after_seconds: float = 0) -> Case[PhotoPage]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(search_photos_retry, '__module__', "dex_connectors.unsplash.unsplashmock")

def search_photos_searched_default_photos() -> Case[PhotoPage]:
    """Return the manifest searchedDefaultPhotos outcome."""
    return Case.branch_result(SEARCHED, PhotoPage(total=1, total_pages=1, photos=(Photo(id='office-1', description='A sunlit office', alt_description='sunlit office interior', html_url='https://unsplash.com/photos/office-1', regular_url='https://images.unsplash.com/photo-office-1', photographer_name='A. Photographer', photographer_url='https://unsplash.com/@photographer'),)))

setattr(search_photos_searched_default_photos, '__module__', "dex_connectors.unsplash.unsplashmock")

def get_photo_found(value: Photo, failure: Failure | None = None) -> Case[Photo]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_photo_found, '__module__', "dex_connectors.unsplash.unsplashmock")

def get_photo_not_found(value: Photo, failure: Failure | None = None) -> Case[Photo]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_photo_not_found, '__module__', "dex_connectors.unsplash.unsplashmock")

def get_photo_provider_rejected(value: Photo, failure: Failure | None = None) -> Case[Photo]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_photo_provider_rejected, '__module__', "dex_connectors.unsplash.unsplashmock")

def get_photo_invalid_response(value: Photo, failure: Failure | None = None) -> Case[Photo]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_photo_invalid_response, '__module__', "dex_connectors.unsplash.unsplashmock")

def get_photo_defect(value: Photo, failure: Failure | None = None) -> Case[Photo]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_photo_defect, '__module__', "dex_connectors.unsplash.unsplashmock")

def get_photo_retry(failure: Failure, after_seconds: float = 0) -> Case[Photo]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_photo_retry, '__module__', "dex_connectors.unsplash.unsplashmock")

def get_photo_found_default_photo() -> Case[Photo]:
    """Return the manifest foundDefaultPhoto outcome."""
    return Case.branch_result(FOUND, Photo(id='office-1', description='A sunlit office', alt_description='sunlit office interior', html_url='https://unsplash.com/photos/office-1', regular_url='https://images.unsplash.com/photo-office-1', photographer_name='A. Photographer', photographer_url='https://unsplash.com/@photographer'))

setattr(get_photo_found_default_photo, '__module__', "dex_connectors.unsplash.unsplashmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "unsplash") -> None:
        self.search_photos_script: Query[SearchPhotosInput, PhotoPage] = Query(SEARCH_PHOTOS_DEFINITION)
        self.search_photos_script.default(search_photos_searched_default_photos())
        self.get_photo_script: Query[GetPhotoInput, Photo] = Query(GET_PHOTO_DEFINITION)
        self.get_photo_script.default(get_photo_found_default_photo())
        self._connection = new_connection_with_operations(
            search_photos=self.search_photos_script,
            get_photo=self.get_photo_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.unsplash.unsplashmock")

__all__ = [
    "Mock",
    "search_photos_searched",
    "search_photos_provider_rejected",
    "search_photos_invalid_response",
    "search_photos_defect",
    "search_photos_retry",
    "search_photos_searched_default_photos",
    "get_photo_found",
    "get_photo_not_found",
    "get_photo_provider_rejected",
    "get_photo_invalid_response",
    "get_photo_defect",
    "get_photo_retry",
    "get_photo_found_default_photo",
]
