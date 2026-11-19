# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListShopsInput, ListShopsOutput, Shop
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_SHOPS_DEFINITION, PROVIDER_REJECTED

def list_shops_found(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_shops_found, '__module__', "dex_connectors.jushuitan.jushuitanmock")

def list_shops_provider_rejected(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_shops_provider_rejected, '__module__', "dex_connectors.jushuitan.jushuitanmock")

def list_shops_invalid_response(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_shops_invalid_response, '__module__', "dex_connectors.jushuitan.jushuitanmock")

def list_shops_defect(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_shops_defect, '__module__', "dex_connectors.jushuitan.jushuitanmock")

def list_shops_retry(failure: Failure, after_seconds: float = 0) -> Case[ListShopsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_shops_retry, '__module__', "dex_connectors.jushuitan.jushuitanmock")

def list_shops_two_shops() -> Case[ListShopsOutput]:
    return Case.branch_result(FOUND, ListShopsOutput(page_index=1, page_size=10, has_next=False, shops=(Shop(shop_id=10178845, name='Example Shop', site='Own store'), Shop(shop_id=10178846, name='Second Shop', site='Marketplace'))))

setattr(list_shops_two_shops, '__module__', "dex_connectors.jushuitan.jushuitanmock")


class Mock:

    def __init__(self, connection_name: str = "jushuitan") -> None:
        self.list_shops_script: Query[ListShopsInput, ListShopsOutput] = Query(LIST_SHOPS_DEFINITION)
        self.list_shops_script.default(list_shops_two_shops())
        self._connection = new_connection_with_operations(
            list_shops=self.list_shops_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.jushuitan.jushuitanmock")

__all__ = [
    "Mock",
    "list_shops_found",
    "list_shops_provider_rejected",
    "list_shops_invalid_response",
    "list_shops_defect",
    "list_shops_retry",
    "list_shops_two_shops",
]
