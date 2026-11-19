# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetPatentValuationInput, GetPatentValuationOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_PATENT_VALUATION_DEFINITION, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def get_patent_valuation_found(value: GetPatentValuationOutput, failure: Failure | None = None) -> Case[GetPatentValuationOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_patent_valuation_found, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

def get_patent_valuation_no_data(value: GetPatentValuationOutput, failure: Failure | None = None) -> Case[GetPatentValuationOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_patent_valuation_no_data, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

def get_patent_valuation_provider_rejected(value: GetPatentValuationOutput, failure: Failure | None = None) -> Case[GetPatentValuationOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_patent_valuation_provider_rejected, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

def get_patent_valuation_invalid_response(value: GetPatentValuationOutput, failure: Failure | None = None) -> Case[GetPatentValuationOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_patent_valuation_invalid_response, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

def get_patent_valuation_defect(value: GetPatentValuationOutput, failure: Failure | None = None) -> Case[GetPatentValuationOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_patent_valuation_defect, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

def get_patent_valuation_retry(failure: Failure, after_seconds: float = 0) -> Case[GetPatentValuationOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_patent_valuation_retry, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

def get_patent_valuation_sample_valuation() -> Case[GetPatentValuationOutput]:
    return Case.branch_result(FOUND, GetPatentValuationOutput(publication_number='JP6012277B2', patent_id='92ccf76a-a718-47e9-8cd6-6c5c9df19ae3', patent_value_usd=43000, legal_score=71, market_score=18, economy_score=23, strategic_score=71, technology_score=28))

setattr(get_patent_valuation_sample_valuation, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

def get_patent_valuation_no_valuation() -> Case[GetPatentValuationOutput]:
    return Case.branch_result(NO_DATA, GetPatentValuationOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getPatentValuation", "Zhihuiya did not find a valuation."))

setattr(get_patent_valuation_no_valuation, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")


class Mock:

    def __init__(self, connection_name: str = "zhihuiya") -> None:
        self.get_patent_valuation_script: Query[GetPatentValuationInput, GetPatentValuationOutput] = Query(GET_PATENT_VALUATION_DEFINITION)
        self.get_patent_valuation_script.default(get_patent_valuation_sample_valuation())
        self._connection = new_connection_with_operations(
            get_patent_valuation=self.get_patent_valuation_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.zhihuiya.zhihuiyamock")

__all__ = [
    "Mock",
    "get_patent_valuation_found",
    "get_patent_valuation_no_data",
    "get_patent_valuation_provider_rejected",
    "get_patent_valuation_invalid_response",
    "get_patent_valuation_defect",
    "get_patent_valuation_retry",
    "get_patent_valuation_sample_valuation",
    "get_patent_valuation_no_valuation",
]
