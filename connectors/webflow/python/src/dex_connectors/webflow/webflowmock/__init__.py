# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Webflow scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef
from dex_connectors.sdk.connectormock import Case, Query

from ..client import CONNECTOR_ID, GET_COLLECTION_ITEM_DEFINITION, LIST_COLLECTION_ITEMS_DEFINITION
from ..connection import Connection, new_connection_with_operations
from ..models import (
    GetCollectionItemInput,
    GetCollectionItemOutput,
    ListCollectionItemsInput,
    ListCollectionItemsOutput,
)


def list_collection_items_default_list_collection_items() -> Case[ListCollectionItemsOutput]:
    """Return the manifest's listCollectionItems defaultListCollectionItems answer."""
    return Case.branch_result(
        "listed",
        ListCollectionItemsOutput(
            {
                "items": [{"id": "62b720ef280c7a7a3be8cabe", "fieldData": {"name": "Meridian"}}],
                "pagination": {"total": 1, "limit": 100, "offset": 0},
            }
        ),
    )


def get_collection_item_default_get_collection_item() -> Case[GetCollectionItemOutput]:
    """Return the manifest's getCollectionItem defaultGetCollectionItem answer."""
    return Case.branch_result(
        "found",
        GetCollectionItemOutput({"id": "62b720ef280c7a7a3be8cabe", "fieldData": {"name": "Meridian"}}),
    )


class Mock:
    """Script Webflow Queries, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "webflow") -> None:
        self.list_collection_items_script: Query[ListCollectionItemsInput, ListCollectionItemsOutput] = Query(
            LIST_COLLECTION_ITEMS_DEFINITION
        )
        self.list_collection_items_script.default(list_collection_items_default_list_collection_items())
        self.get_collection_item_script: Query[GetCollectionItemInput, GetCollectionItemOutput] = Query(
            GET_COLLECTION_ITEM_DEFINITION
        )
        self.get_collection_item_script.default(get_collection_item_default_get_collection_item())
        self._connection = new_connection_with_operations(
            self.list_collection_items_script,
            self.get_collection_item_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_collection_items_default_list_collection_items",
    "get_collection_item_default_get_collection_item",
]
