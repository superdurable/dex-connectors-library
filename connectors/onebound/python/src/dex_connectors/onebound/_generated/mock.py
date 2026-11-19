# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ProductSearchItem, ProductSearchPage, SearchProductsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, PROVIDER_REJECTED, SEARCH_PRODUCTS_DEFINITION

def search_products_listed(value: ProductSearchPage, failure: Failure | None = None) -> Case[ProductSearchPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(search_products_listed, '__module__', "dex_connectors.onebound.oneboundmock")

def search_products_provider_rejected(value: ProductSearchPage, failure: Failure | None = None) -> Case[ProductSearchPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(search_products_provider_rejected, '__module__', "dex_connectors.onebound.oneboundmock")

def search_products_invalid_response(value: ProductSearchPage, failure: Failure | None = None) -> Case[ProductSearchPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(search_products_invalid_response, '__module__', "dex_connectors.onebound.oneboundmock")

def search_products_defect(value: ProductSearchPage, failure: Failure | None = None) -> Case[ProductSearchPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(search_products_defect, '__module__', "dex_connectors.onebound.oneboundmock")

def search_products_retry(failure: Failure, after_seconds: float = 0) -> Case[ProductSearchPage]:
    return Case.retry(failure, after_seconds)

setattr(search_products_retry, '__module__', "dex_connectors.onebound.oneboundmock")

def search_products_listed_product() -> Case[ProductSearchPage]:
    return Case.branch_result(LISTED, ProductSearchPage(page_number=1, total=1, products=(ProductSearchItem(product_id='123456789', title='Sample work gloves', price='12.00', detail_url='https://detail.1688.com/offer/123456789.html'),)))

setattr(search_products_listed_product, '__module__', "dex_connectors.onebound.oneboundmock")


class Mock:

    def __init__(self, connection_name: str = "onebound") -> None:
        self.search_products_script: Query[SearchProductsInput, ProductSearchPage] = Query(SEARCH_PRODUCTS_DEFINITION)
        self.search_products_script.default(search_products_listed_product())
        self._connection = new_connection_with_operations(
            search_products=self.search_products_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.onebound.oneboundmock")

__all__ = [
    "Mock",
    "search_products_listed",
    "search_products_provider_rejected",
    "search_products_invalid_response",
    "search_products_defect",
    "search_products_retry",
    "search_products_listed_product",
]
