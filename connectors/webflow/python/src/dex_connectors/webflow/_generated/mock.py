# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated manifest-backed provider outcomes and scripts."""

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetCollectionItemInput, GetCollectionItemOutput, ListCollectionItemsInput, ListCollectionItemsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_COLLECTION_ITEM_DEFINITION, INVALID_RESPONSE, LISTED, LIST_COLLECTION_ITEMS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_collection_items_listed(value: ListCollectionItemsOutput, failure: Failure | None = None) -> Case[ListCollectionItemsOutput]:
    """Script the listed branch."""
    return Case.branch_result(LISTED, value, failure)

setattr(list_collection_items_listed, '__module__', "dex_connectors.webflow.webflowmock")

def list_collection_items_provider_rejected(value: ListCollectionItemsOutput, failure: Failure | None = None) -> Case[ListCollectionItemsOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_collection_items_provider_rejected, '__module__', "dex_connectors.webflow.webflowmock")

def list_collection_items_invalid_response(value: ListCollectionItemsOutput, failure: Failure | None = None) -> Case[ListCollectionItemsOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_collection_items_invalid_response, '__module__', "dex_connectors.webflow.webflowmock")

def list_collection_items_defect(value: ListCollectionItemsOutput, failure: Failure | None = None) -> Case[ListCollectionItemsOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(list_collection_items_defect, '__module__', "dex_connectors.webflow.webflowmock")

def list_collection_items_retry(failure: Failure, after_seconds: float = 0) -> Case[ListCollectionItemsOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(list_collection_items_retry, '__module__', "dex_connectors.webflow.webflowmock")

def list_collection_items_default_list_collection_items() -> Case[ListCollectionItemsOutput]:
    """Return the manifest defaultListCollectionItems outcome."""
    return Case.branch_result(LISTED, ListCollectionItemsOutput(data={'items': [{'id': '62b720ef280c7a7a3be8cabe', 'fieldData': {'name': 'Meridian'}}], 'pagination': {'total': 1, 'limit': 100, 'offset': 0}}))

setattr(list_collection_items_default_list_collection_items, '__module__', "dex_connectors.webflow.webflowmock")

def get_collection_item_found(value: GetCollectionItemOutput, failure: Failure | None = None) -> Case[GetCollectionItemOutput]:
    """Script the found branch."""
    return Case.branch_result(FOUND, value, failure)

setattr(get_collection_item_found, '__module__', "dex_connectors.webflow.webflowmock")

def get_collection_item_not_found(value: GetCollectionItemOutput, failure: Failure | None = None) -> Case[GetCollectionItemOutput]:
    """Script the notFound branch."""
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_collection_item_not_found, '__module__', "dex_connectors.webflow.webflowmock")

def get_collection_item_provider_rejected(value: GetCollectionItemOutput, failure: Failure | None = None) -> Case[GetCollectionItemOutput]:
    """Script the providerRejected branch."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_collection_item_provider_rejected, '__module__', "dex_connectors.webflow.webflowmock")

def get_collection_item_invalid_response(value: GetCollectionItemOutput, failure: Failure | None = None) -> Case[GetCollectionItemOutput]:
    """Script the invalidResponse branch."""
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_collection_item_invalid_response, '__module__', "dex_connectors.webflow.webflowmock")

def get_collection_item_defect(value: GetCollectionItemOutput, failure: Failure | None = None) -> Case[GetCollectionItemOutput]:
    """Script the defect branch."""
    return Case.branch_result(DEFECT, value, failure)

setattr(get_collection_item_defect, '__module__', "dex_connectors.webflow.webflowmock")

def get_collection_item_retry(failure: Failure, after_seconds: float = 0) -> Case[GetCollectionItemOutput]:
    """Script a retry of this provider operation."""
    return Case.retry(failure, after_seconds)

setattr(get_collection_item_retry, '__module__', "dex_connectors.webflow.webflowmock")

def get_collection_item_default_get_collection_item() -> Case[GetCollectionItemOutput]:
    """Return the manifest defaultGetCollectionItem outcome."""
    return Case.branch_result(FOUND, GetCollectionItemOutput(data={'id': '62b720ef280c7a7a3be8cabe', 'fieldData': {'name': 'Meridian'}}))

setattr(get_collection_item_default_get_collection_item, '__module__', "dex_connectors.webflow.webflowmock")


class Mock:
    """Script manifest outcomes for this connector."""

    def __init__(self, connection_name: str = "webflow") -> None:
        self.list_collection_items_script: Query[ListCollectionItemsInput, ListCollectionItemsOutput] = Query(LIST_COLLECTION_ITEMS_DEFINITION)
        self.list_collection_items_script.default(list_collection_items_default_list_collection_items())
        self.get_collection_item_script: Query[GetCollectionItemInput, GetCollectionItemOutput] = Query(GET_COLLECTION_ITEM_DEFINITION)
        self.get_collection_item_script.default(get_collection_item_default_get_collection_item())
        self._connection = new_connection_with_operations(
            list_collection_items=self.list_collection_items_script,
            get_collection_item=self.get_collection_item_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock connection for Dex Step registration."""
        return self._connection

setattr(Mock, '__module__', "dex_connectors.webflow.webflowmock")

__all__ = [
    "Mock",
    "list_collection_items_listed",
    "list_collection_items_provider_rejected",
    "list_collection_items_invalid_response",
    "list_collection_items_defect",
    "list_collection_items_retry",
    "list_collection_items_default_list_collection_items",
    "get_collection_item_found",
    "get_collection_item_not_found",
    "get_collection_item_provider_rejected",
    "get_collection_item_invalid_response",
    "get_collection_item_defect",
    "get_collection_item_retry",
    "get_collection_item_default_get_collection_item",
]
