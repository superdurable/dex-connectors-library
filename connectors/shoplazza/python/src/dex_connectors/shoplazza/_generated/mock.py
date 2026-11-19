# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetShopInput, GetShopOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SHOP_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_shop_found(value: GetShopOutput, failure: Failure | None = None) -> Case[GetShopOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_shop_found, '__module__', "dex_connectors.shoplazza.shoplazzamock")

def get_shop_provider_rejected(value: GetShopOutput, failure: Failure | None = None) -> Case[GetShopOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_shop_provider_rejected, '__module__', "dex_connectors.shoplazza.shoplazzamock")

def get_shop_invalid_response(value: GetShopOutput, failure: Failure | None = None) -> Case[GetShopOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_shop_invalid_response, '__module__', "dex_connectors.shoplazza.shoplazzamock")

def get_shop_defect(value: GetShopOutput, failure: Failure | None = None) -> Case[GetShopOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_shop_defect, '__module__', "dex_connectors.shoplazza.shoplazzamock")

def get_shop_retry(failure: Failure, after_seconds: float = 0) -> Case[GetShopOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_shop_retry, '__module__', "dex_connectors.shoplazza.shoplazzamock")

def get_shop_sample_shop() -> Case[GetShopOutput]:
    return Case.branch_result(FOUND, GetShopOutput(id=42, name='Sample Shop', domain='sample.example', currency='USD'))

setattr(get_shop_sample_shop, '__module__', "dex_connectors.shoplazza.shoplazzamock")


class Mock:

    def __init__(self, connection_name: str = "shoplazza") -> None:
        self.get_shop_script: Query[GetShopInput, GetShopOutput] = Query(GET_SHOP_DEFINITION)
        self.get_shop_script.default(get_shop_sample_shop())
        self._connection = new_connection_with_operations(
            get_shop=self.get_shop_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.shoplazza.shoplazzamock")

__all__ = [
    "Mock",
    "get_shop_found",
    "get_shop_provider_rejected",
    "get_shop_invalid_response",
    "get_shop_defect",
    "get_shop_retry",
    "get_shop_sample_shop",
]
