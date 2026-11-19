# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListStockCompaniesInput, ListStockCompaniesOutput, StockCompany
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_STOCK_COMPANIES_DEFINITION, PROVIDER_REJECTED

def list_stock_companies_listed(value: ListStockCompaniesOutput, failure: Failure | None = None) -> Case[ListStockCompaniesOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_stock_companies_listed, '__module__', "dex_connectors.lixinger.lixingermock")

def list_stock_companies_provider_rejected(value: ListStockCompaniesOutput, failure: Failure | None = None) -> Case[ListStockCompaniesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_stock_companies_provider_rejected, '__module__', "dex_connectors.lixinger.lixingermock")

def list_stock_companies_invalid_response(value: ListStockCompaniesOutput, failure: Failure | None = None) -> Case[ListStockCompaniesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_stock_companies_invalid_response, '__module__', "dex_connectors.lixinger.lixingermock")

def list_stock_companies_defect(value: ListStockCompaniesOutput, failure: Failure | None = None) -> Case[ListStockCompaniesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_stock_companies_defect, '__module__', "dex_connectors.lixinger.lixingermock")

def list_stock_companies_retry(failure: Failure, after_seconds: float = 0) -> Case[ListStockCompaniesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_stock_companies_retry, '__module__', "dex_connectors.lixinger.lixingermock")

def list_stock_companies_sample_stock_companies() -> Case[ListStockCompaniesOutput]:
    return Case.branch_result(LISTED, ListStockCompaniesOutput(companies=(StockCompany(stock_code='300750', name='Example Company', exchange='sz', listing_status='normally_listed'),)))

setattr(list_stock_companies_sample_stock_companies, '__module__', "dex_connectors.lixinger.lixingermock")


class Mock:

    def __init__(self, connection_name: str = "lixinger") -> None:
        self.list_stock_companies_script: Query[ListStockCompaniesInput, ListStockCompaniesOutput] = Query(LIST_STOCK_COMPANIES_DEFINITION)
        self.list_stock_companies_script.default(list_stock_companies_sample_stock_companies())
        self._connection = new_connection_with_operations(
            list_stock_companies=self.list_stock_companies_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.lixinger.lixingermock")

__all__ = [
    "Mock",
    "list_stock_companies_listed",
    "list_stock_companies_provider_rejected",
    "list_stock_companies_invalid_response",
    "list_stock_companies_defect",
    "list_stock_companies_retry",
    "list_stock_companies_sample_stock_companies",
]
