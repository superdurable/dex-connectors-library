# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetShopBasicInput, ShopBasic
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SHOP_BASIC_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_shop_basic_found(value: ShopBasic, failure: Failure | None = None) -> Case[ShopBasic]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_shop_basic_found, '__module__', "dex_connectors.youzan.youzanmock")

def get_shop_basic_provider_rejected(value: ShopBasic, failure: Failure | None = None) -> Case[ShopBasic]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_shop_basic_provider_rejected, '__module__', "dex_connectors.youzan.youzanmock")

def get_shop_basic_invalid_response(value: ShopBasic, failure: Failure | None = None) -> Case[ShopBasic]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_shop_basic_invalid_response, '__module__', "dex_connectors.youzan.youzanmock")

def get_shop_basic_defect(value: ShopBasic, failure: Failure | None = None) -> Case[ShopBasic]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_shop_basic_defect, '__module__', "dex_connectors.youzan.youzanmock")

def get_shop_basic_retry(failure: Failure, after_seconds: float = 0) -> Case[ShopBasic]:
    return Case.retry(failure, after_seconds)

setattr(get_shop_basic_retry, '__module__', "dex_connectors.youzan.youzanmock")

def get_shop_basic_found_default() -> Case[ShopBasic]:
    return Case.branch_result(FOUND, ShopBasic(shop_id=59931296, name='Sample Shop', url='https://h5.youzan.com/v2/showcase/homepage?alias=sample', role=1, line_of_business=70000))

setattr(get_shop_basic_found_default, '__module__', "dex_connectors.youzan.youzanmock")


class Mock:

    def __init__(self, connection_name: str = "youzan") -> None:
        self.get_shop_basic_script: Query[GetShopBasicInput, ShopBasic] = Query(GET_SHOP_BASIC_DEFINITION)
        self.get_shop_basic_script.default(get_shop_basic_found_default())
        self._connection = new_connection_with_operations(
            get_shop_basic=self.get_shop_basic_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.youzan.youzanmock")

__all__ = [
    "Mock",
    "get_shop_basic_found",
    "get_shop_basic_provider_rejected",
    "get_shop_basic_invalid_response",
    "get_shop_basic_defect",
    "get_shop_basic_retry",
    "get_shop_basic_found_default",
]
