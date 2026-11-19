# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListProductsInput, Product, ProductList
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_PRODUCTS_DEFINITION, PROVIDER_REJECTED

def list_products_listed(value: ProductList, failure: Failure | None = None) -> Case[ProductList]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_products_listed, '__module__', "dex_connectors.zentao.zentaomock")

def list_products_provider_rejected(value: ProductList, failure: Failure | None = None) -> Case[ProductList]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_products_provider_rejected, '__module__', "dex_connectors.zentao.zentaomock")

def list_products_invalid_response(value: ProductList, failure: Failure | None = None) -> Case[ProductList]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_products_invalid_response, '__module__', "dex_connectors.zentao.zentaomock")

def list_products_defect(value: ProductList, failure: Failure | None = None) -> Case[ProductList]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_products_defect, '__module__', "dex_connectors.zentao.zentaomock")

def list_products_retry(failure: Failure, after_seconds: float = 0) -> Case[ProductList]:
    return Case.retry(failure, after_seconds)

setattr(list_products_retry, '__module__', "dex_connectors.zentao.zentaomock")

def list_products_listed_product() -> Case[ProductList]:
    return Case.branch_result(LISTED, ProductList(total=1, products=(Product(id=6, name='Example Product', code='example', type='normal', access_control='private'),)))

setattr(list_products_listed_product, '__module__', "dex_connectors.zentao.zentaomock")


class Mock:

    def __init__(self, connection_name: str = "zentao") -> None:
        self.list_products_script: Query[ListProductsInput, ProductList] = Query(LIST_PRODUCTS_DEFINITION)
        self.list_products_script.default(list_products_listed_product())
        self._connection = new_connection_with_operations(
            list_products=self.list_products_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zentao.zentaomock")

__all__ = [
    "Mock",
    "list_products_listed",
    "list_products_provider_rejected",
    "list_products_invalid_response",
    "list_products_defect",
    "list_products_retry",
    "list_products_listed_product",
]
