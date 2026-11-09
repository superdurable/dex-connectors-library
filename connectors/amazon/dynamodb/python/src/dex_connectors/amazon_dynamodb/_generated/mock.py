# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetItemInput, ItemResult
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_ITEM_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_item_found(value: ItemResult, failure: Failure | None = None) -> Case[ItemResult]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_item_found, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")

def get_item_missing(value: ItemResult, failure: Failure | None = None) -> Case[ItemResult]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_item_missing, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")

def get_item_provider_rejected(value: ItemResult, failure: Failure | None = None) -> Case[ItemResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_item_provider_rejected, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")

def get_item_invalid_response(value: ItemResult, failure: Failure | None = None) -> Case[ItemResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_item_invalid_response, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")

def get_item_defect(value: ItemResult, failure: Failure | None = None) -> Case[ItemResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_item_defect, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")

def get_item_retry(failure: Failure, after_seconds: float = 0) -> Case[ItemResult]:
    return Case.retry(failure, after_seconds)

setattr(get_item_retry, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")

def get_item_found_default() -> Case[ItemResult]:
    return Case.branch_result(FOUND, ItemResult(exists=True, item={'pk': {'S': 'example'}}))

setattr(get_item_found_default, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")


class Mock:

    def __init__(self, connection_name: str = "amazon-dynamodb") -> None:
        self.get_item_script: Query[GetItemInput, ItemResult] = Query(GET_ITEM_DEFINITION)
        self.get_item_script.default(get_item_found_default())
        self._connection = new_connection_with_operations(
            get_item=self.get_item_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amazon_dynamodb.dynamodbmock")

__all__ = [
    "Mock",
    "get_item_found",
    "get_item_missing",
    "get_item_provider_rejected",
    "get_item_invalid_response",
    "get_item_defect",
    "get_item_retry",
    "get_item_found_default",
]
