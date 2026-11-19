# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import SearchWebInput, SearchWebOutput, WebResult
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, SEARCH_WEB_DEFINITION

def search_web_found(value: SearchWebOutput, failure: Failure | None = None) -> Case[SearchWebOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(search_web_found, '__module__', "dex_connectors.bocha.bochamock")

def search_web_provider_rejected(value: SearchWebOutput, failure: Failure | None = None) -> Case[SearchWebOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_web_provider_rejected, '__module__', "dex_connectors.bocha.bochamock")

def search_web_invalid_response(value: SearchWebOutput, failure: Failure | None = None) -> Case[SearchWebOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_web_invalid_response, '__module__', "dex_connectors.bocha.bochamock")

def search_web_defect(value: SearchWebOutput, failure: Failure | None = None) -> Case[SearchWebOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_web_defect, '__module__', "dex_connectors.bocha.bochamock")

def search_web_retry(failure: Failure, after_seconds: float = 0) -> Case[SearchWebOutput]:
    return Case.retry(failure, after_seconds)

setattr(search_web_retry, '__module__', "dex_connectors.bocha.bochamock")

def search_web_search_results() -> Case[SearchWebOutput]:
    return Case.branch_result(FOUND, SearchWebOutput(results=(WebResult(title='Example page', url='https://example.com/article', snippet='Example search excerpt.'),)))

setattr(search_web_search_results, '__module__', "dex_connectors.bocha.bochamock")

def search_web_no_results() -> Case[SearchWebOutput]:
    return Case.branch_result(FOUND, SearchWebOutput(results=()))

setattr(search_web_no_results, '__module__', "dex_connectors.bocha.bochamock")


class Mock:

    def __init__(self, connection_name: str = "bocha") -> None:
        self.search_web_script: Query[SearchWebInput, SearchWebOutput] = Query(SEARCH_WEB_DEFINITION)
        self.search_web_script.default(search_web_search_results())
        self._connection = new_connection_with_operations(
            search_web=self.search_web_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.bocha.bochamock")

__all__ = [
    "Mock",
    "search_web_found",
    "search_web_provider_rejected",
    "search_web_invalid_response",
    "search_web_defect",
    "search_web_retry",
    "search_web_search_results",
    "search_web_no_results",
]
