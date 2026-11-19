# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import QueryShopInput, ShopResult
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_SHOP_DEFINITION

def query_shop_found(value: ShopResult, failure: Failure | None = None) -> Case[ShopResult]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_shop_found, '__module__', "dex_connectors.easyapi.easyapimock")

def query_shop_provider_rejected(value: ShopResult, failure: Failure | None = None) -> Case[ShopResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_shop_provider_rejected, '__module__', "dex_connectors.easyapi.easyapimock")

def query_shop_invalid_response(value: ShopResult, failure: Failure | None = None) -> Case[ShopResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_shop_invalid_response, '__module__', "dex_connectors.easyapi.easyapimock")

def query_shop_defect(value: ShopResult, failure: Failure | None = None) -> Case[ShopResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_shop_defect, '__module__', "dex_connectors.easyapi.easyapimock")

def query_shop_retry(failure: Failure, after_seconds: float = 0) -> Case[ShopResult]:
    return Case.retry(failure, after_seconds)

setattr(query_shop_retry, '__module__', "dex_connectors.easyapi.easyapimock")

def query_shop_found_shop() -> Case[ShopResult]:
    return Case.branch_result(FOUND, ShopResult(shop_id=1001, name='Example Company', api_state=1, end_time='2027-01-01 00:00:00'))

setattr(query_shop_found_shop, '__module__', "dex_connectors.easyapi.easyapimock")


class Mock:

    def __init__(self, connection_name: str = "easyapi") -> None:
        self.query_shop_script: Query[QueryShopInput, ShopResult] = Query(QUERY_SHOP_DEFINITION)
        self.query_shop_script.default(query_shop_found_shop())
        self._connection = new_connection_with_operations(
            query_shop=self.query_shop_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.easyapi.easyapimock")

__all__ = [
    "Mock",
    "query_shop_found",
    "query_shop_provider_rejected",
    "query_shop_invalid_response",
    "query_shop_defect",
    "query_shop_retry",
    "query_shop_found_shop",
]
