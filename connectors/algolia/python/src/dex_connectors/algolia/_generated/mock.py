# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import SaveObjectInput, SavedObject, SearchHit, SearchInput, SearchPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, PROVIDER_REJECTED, SAVED, SAVE_OBJECT_DEFINITION, SEARCHED, SEARCH_DEFINITION

def save_object_saved(value: SavedObject, failure: Failure | None = None) -> Case[SavedObject]:
    return Case.branch_result(SAVED, value, failure)

setattr(save_object_saved, '__module__', "dex_connectors.algolia.algoliamock")

def save_object_provider_rejected(value: SavedObject, failure: Failure | None = None) -> Case[SavedObject]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(save_object_provider_rejected, '__module__', "dex_connectors.algolia.algoliamock")

def save_object_invalid_response(value: SavedObject, failure: Failure | None = None) -> Case[SavedObject]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(save_object_invalid_response, '__module__', "dex_connectors.algolia.algoliamock")

def save_object_defect(value: SavedObject, failure: Failure | None = None) -> Case[SavedObject]:
    return Case.branch_result(DEFECT, value, failure)

setattr(save_object_defect, '__module__', "dex_connectors.algolia.algoliamock")

def save_object_retry(failure: Failure, after_seconds: float = 0) -> Case[SavedObject]:
    return Case.retry(failure, after_seconds)

setattr(save_object_retry, '__module__', "dex_connectors.algolia.algoliamock")

def save_object_saved_default_object() -> Case[SavedObject]:
    return Case.branch_result(SAVED, SavedObject(object_id='product-1', task_id=1514562690001))

setattr(save_object_saved_default_object, '__module__', "dex_connectors.algolia.algoliamock")

def search_searched(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(SEARCHED, value, failure)

setattr(search_searched, '__module__', "dex_connectors.algolia.algoliamock")

def search_provider_rejected(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_provider_rejected, '__module__', "dex_connectors.algolia.algoliamock")

def search_invalid_response(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_invalid_response, '__module__', "dex_connectors.algolia.algoliamock")

def search_defect(value: SearchPage, failure: Failure | None = None) -> Case[SearchPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_defect, '__module__', "dex_connectors.algolia.algoliamock")

def search_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchPage]:
    return Case.retry(failure, after_seconds)

setattr(search_retry, '__module__', "dex_connectors.algolia.algoliamock")

def search_searched_default_index() -> Case[SearchPage]:
    return Case.branch_result(SEARCHED, SearchPage(nb_hits=1, hits=(SearchHit(object_id='product-1', name='Meridian notebook'),)))

setattr(search_searched_default_index, '__module__', "dex_connectors.algolia.algoliamock")


class Mock:

    def __init__(self, connection_name: str = "algolia") -> None:
        self.save_object_script: Mutation[SaveObjectInput, SavedObject] = Mutation(SAVE_OBJECT_DEFINITION)
        self.save_object_script.default(save_object_saved_default_object())
        self.search_script: Query[SearchInput, SearchPage] = Query(SEARCH_DEFINITION)
        self.search_script.default(search_searched_default_index())
        self._connection = new_connection_with_operations(
            save_object=self.save_object_script,
            search=self.search_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.algolia.algoliamock")

__all__ = [
    "Mock",
    "save_object_saved",
    "save_object_provider_rejected",
    "save_object_invalid_response",
    "save_object_defect",
    "save_object_retry",
    "save_object_saved_default_object",
    "search_searched",
    "search_provider_rejected",
    "search_invalid_response",
    "search_defect",
    "search_retry",
    "search_searched_default_index",
]
