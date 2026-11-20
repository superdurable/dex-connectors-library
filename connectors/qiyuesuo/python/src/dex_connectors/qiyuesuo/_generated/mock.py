# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ContractSummary, ListContractsInput, ListContractsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_CONTRACTS_DEFINITION, PROVIDER_REJECTED

def list_contracts_listed(value: ListContractsOutput, failure: Failure | None = None) -> Case[ListContractsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_contracts_listed, '__module__', "dex_connectors.qiyuesuo.qiyuesuomock")

def list_contracts_provider_rejected(value: ListContractsOutput, failure: Failure | None = None) -> Case[ListContractsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_contracts_provider_rejected, '__module__', "dex_connectors.qiyuesuo.qiyuesuomock")

def list_contracts_invalid_response(value: ListContractsOutput, failure: Failure | None = None) -> Case[ListContractsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_contracts_invalid_response, '__module__', "dex_connectors.qiyuesuo.qiyuesuomock")

def list_contracts_defect(value: ListContractsOutput, failure: Failure | None = None) -> Case[ListContractsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_contracts_defect, '__module__', "dex_connectors.qiyuesuo.qiyuesuomock")

def list_contracts_retry(failure: Failure, after_seconds: float = 0) -> Case[ListContractsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_contracts_retry, '__module__', "dex_connectors.qiyuesuo.qiyuesuomock")

def list_contracts_sample_contracts() -> Case[ListContractsOutput]:
    return Case.branch_result(LISTED, ListContractsOutput(contracts=(ContractSummary(id='2966583458579853505', status='SIGNING'),), total_count=1, next_offset=0))

setattr(list_contracts_sample_contracts, '__module__', "dex_connectors.qiyuesuo.qiyuesuomock")


class Mock:

    def __init__(self, connection_name: str = "qiyuesuo") -> None:
        self.list_contracts_script: Query[ListContractsInput, ListContractsOutput] = Query(LIST_CONTRACTS_DEFINITION)
        self.list_contracts_script.default(list_contracts_sample_contracts())
        self._connection = new_connection_with_operations(
            list_contracts=self.list_contracts_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.qiyuesuo.qiyuesuomock")

__all__ = [
    "Mock",
    "list_contracts_listed",
    "list_contracts_provider_rejected",
    "list_contracts_invalid_response",
    "list_contracts_defect",
    "list_contracts_retry",
    "list_contracts_sample_contracts",
]
