# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Product, ProductPage, SearchProductsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, PROVIDER_REJECTED, SEARCH_PRODUCTS_DEFINITION

def search_products_listed(value: ProductPage, failure: Failure | None = None) -> Case[ProductPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(search_products_listed, '__module__', "dex_connectors.smzdm.smzdmmock")

def search_products_provider_rejected(value: ProductPage, failure: Failure | None = None) -> Case[ProductPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_products_provider_rejected, '__module__', "dex_connectors.smzdm.smzdmmock")

def search_products_invalid_response(value: ProductPage, failure: Failure | None = None) -> Case[ProductPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_products_invalid_response, '__module__', "dex_connectors.smzdm.smzdmmock")

def search_products_defect(value: ProductPage, failure: Failure | None = None) -> Case[ProductPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_products_defect, '__module__', "dex_connectors.smzdm.smzdmmock")

def search_products_retry(failure: Failure, after_seconds: float = 0) -> Case[ProductPage]:
    return Case.retry(failure, after_seconds)

setattr(search_products_retry, '__module__', "dex_connectors.smzdm.smzdmmock")

def search_products_listed_product() -> Case[ProductPage]:
    return Case.branch_result(LISTED, ProductPage(total=1, products=(Product(title='Example Product', price_yuan=99.9),)))

setattr(search_products_listed_product, '__module__', "dex_connectors.smzdm.smzdmmock")


class Mock:

    def __init__(self, connection_name: str = "smzdm") -> None:
        self.search_products_script: Query[SearchProductsInput, ProductPage] = Query(SEARCH_PRODUCTS_DEFINITION)
        self.search_products_script.default(search_products_listed_product())
        self._connection = new_connection_with_operations(
            search_products=self.search_products_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.smzdm.smzdmmock")

__all__ = [
    "Mock",
    "search_products_listed",
    "search_products_provider_rejected",
    "search_products_invalid_response",
    "search_products_defect",
    "search_products_retry",
    "search_products_listed_product",
]
