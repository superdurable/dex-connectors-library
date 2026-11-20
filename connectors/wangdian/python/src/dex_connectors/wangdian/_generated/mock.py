# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListShopsInput, ListShopsOutput, ShopSummary
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_SHOPS_DEFINITION, PROVIDER_REJECTED

def list_shops_listed(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_shops_listed, '__module__', "dex_connectors.wangdian.wangdianmock")

def list_shops_provider_rejected(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_shops_provider_rejected, '__module__', "dex_connectors.wangdian.wangdianmock")

def list_shops_invalid_response(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_shops_invalid_response, '__module__', "dex_connectors.wangdian.wangdianmock")

def list_shops_defect(value: ListShopsOutput, failure: Failure | None = None) -> Case[ListShopsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_shops_defect, '__module__', "dex_connectors.wangdian.wangdianmock")

def list_shops_retry(failure: Failure, after_seconds: float = 0) -> Case[ListShopsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_shops_retry, '__module__', "dex_connectors.wangdian.wangdianmock")

def list_shops_sample_shops() -> Case[ListShopsOutput]:
    return Case.branch_result(LISTED, ListShopsOutput(shops=(ShopSummary(id='332', number='SHOP-332', name='Example Shop', platform_id=127, authorization_state=1),), total=1, next_page=0))

setattr(list_shops_sample_shops, '__module__', "dex_connectors.wangdian.wangdianmock")


class Mock:

    def __init__(self, connection_name: str = "wangdian") -> None:
        self.list_shops_script: Query[ListShopsInput, ListShopsOutput] = Query(LIST_SHOPS_DEFINITION)
        self.list_shops_script.default(list_shops_sample_shops())
        self._connection = new_connection_with_operations(
            list_shops=self.list_shops_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.wangdian.wangdianmock")

__all__ = [
    "Mock",
    "list_shops_listed",
    "list_shops_provider_rejected",
    "list_shops_invalid_response",
    "list_shops_defect",
    "list_shops_retry",
    "list_shops_sample_shops",
]
