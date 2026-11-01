# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Algolia scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..client import CONNECTOR_ID, SAVE_OBJECT_DEFINITION, SAVED, SEARCH_DEFINITION, SEARCHED
from ..connection import Connection, new_connection_with_operations
from ..models import SavedObject, SaveObjectInput, SearchHit, SearchInput, SearchPage


def search_searched(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    """Script the searched route."""
    return Case.branch_result(SEARCHED, value, failure)


def search_default_answer() -> Case[SearchPage]:
    """Return the manifest's search default answer."""
    return search_searched(SearchPage(1, (SearchHit("product-1", "Meridian notebook"),)))


def save_object_saved(value: SavedObject, failure: Failure | None = None) -> Case[SavedObject]:
    """Script the saved route."""
    return Case.branch_result(SAVED, value, failure)


def save_object_default_answer() -> Case[SavedObject]:
    """Return the manifest's saveObject default answer."""
    return save_object_saved(SavedObject("product-1", 1514562690001))


class Mock:
    """Script Algolia operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "algolia") -> None:
        self.search_script: Query[SearchInput, SearchPage] = Query(SEARCH_DEFINITION)
        self.search_script.default(search_default_answer())
        self.save_object_script: Mutation[SaveObjectInput, SavedObject] = Mutation(SAVE_OBJECT_DEFINITION)
        self.save_object_script.default(save_object_default_answer())
        self._connection = new_connection_with_operations(
            self.save_object_script, self.search_script, ConnectionRef(CONNECTOR_ID, connection_name)
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "search_searched",
    "search_default_answer",
    "save_object_saved",
    "save_object_default_answer",
]
