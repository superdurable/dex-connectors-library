# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public manifest-backed mock helpers."""

from dex_connectors.sdk import ConnectionRef as ConnectionRef
from dex_connectors.sdk import Failure as Failure
from dex_connectors.sdk.connectormock import Case as Case
from dex_connectors.sdk.connectormock import Case as _Case
from dex_connectors.sdk.connectormock import Mutation as Mutation
from dex_connectors.sdk.connectormock import Query as Query

from .._generated.mock import Mock as Mock
from .._generated.mock import save_object_saved as save_object_saved
from .._generated.mock import save_object_saved_default_object as _save_object_saved_default_object
from .._generated.mock import search_searched as search_searched
from .._generated.mock import search_searched_default_index as _search_searched_default_index
from ..client import CONNECTOR_ID as CONNECTOR_ID
from ..client import SAVE_OBJECT_DEFINITION as SAVE_OBJECT_DEFINITION
from ..client import SAVED as SAVED
from ..client import SEARCH_DEFINITION as SEARCH_DEFINITION
from ..client import SEARCHED as SEARCHED
from ..connection import Connection as Connection
from ..connection import new_connection_with_operations as new_connection_with_operations
from ..models import SavedObject as SavedObject
from ..models import SavedObject as _SavedObject
from ..models import SaveObjectInput as SaveObjectInput
from ..models import SearchHit as SearchHit
from ..models import SearchInput as SearchInput
from ..models import SearchPage as SearchPage
from ..models import SearchPage as _SearchPage


def save_object_default_answer() -> _Case[_SavedObject]:
    """Return the manifest's saveObject default answer."""
    return _save_object_saved_default_object()


def search_default_answer() -> _Case[_SearchPage]:
    """Return the manifest's search default answer."""
    return _search_searched_default_index()


__all__ = [
    "Mock",
    "search_searched",
    "search_default_answer",
    "save_object_saved",
    "save_object_default_answer",
]
