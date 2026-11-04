# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import get_photo_found as get_photo_found
from .._generated.mock import get_photo_found_default_photo as _get_photo_found_default_photo
from .._generated.mock import get_photo_not_found as get_photo_not_found
from .._generated.mock import search_photos_searched as search_photos_searched
from .._generated.mock import search_photos_searched_default_photos as _search_photos_searched_default_photos
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import FOUND as FOUND
from ..client import GET_PHOTO_DEFINITION as GET_PHOTO_DEFINITION
from ..client import NOT_FOUND as NOT_FOUND
from ..client import SEARCH_PHOTOS_DEFINITION as SEARCH_PHOTOS_DEFINITION
from ..client import SEARCHED as SEARCHED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import GetPhotoInput as GetPhotoInput
from ..models import Photo as Photo
from ..models import Photo as _Photo
from ..models import PhotoPage as PhotoPage
from ..models import PhotoPage as _PhotoPage
from ..models import SearchPhotosInput as SearchPhotosInput


def get_photo_default_answer() -> _Case[_Photo]:
    """Return the manifest's getPhoto default answer."""
    return _get_photo_found_default_photo()


def search_photos_default_answer() -> _Case[_PhotoPage]:
    """Return the manifest's searchPhotos default answer."""
    return _search_photos_searched_default_photos()


__all__ = [
    "Mock",
    "search_photos_searched",
    "search_photos_default_answer",
    "get_photo_found",
    "get_photo_not_found",
    "get_photo_default_answer",
]

Mock.__doc__ = "Script Unsplash Queries, defaulting to their manifest answers."

search_photos_searched.__doc__ = "Script the searched route."
get_photo_found.__doc__ = "Script the successful found route."
get_photo_not_found.__doc__ = "Script a missing photo."
