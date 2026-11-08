# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ContractStatus, GetContractInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CONTRACT_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_contract_found(value: ContractStatus, failure: Failure | None = None) -> Case[ContractStatus]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_contract_found, '__module__', "dex_connectors.deel.deelmock")

def get_contract_not_found(value: ContractStatus, failure: Failure | None = None) -> Case[ContractStatus]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_contract_not_found, '__module__', "dex_connectors.deel.deelmock")

def get_contract_provider_rejected(value: ContractStatus, failure: Failure | None = None) -> Case[ContractStatus]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_contract_provider_rejected, '__module__', "dex_connectors.deel.deelmock")

def get_contract_invalid_response(value: ContractStatus, failure: Failure | None = None) -> Case[ContractStatus]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_contract_invalid_response, '__module__', "dex_connectors.deel.deelmock")

def get_contract_defect(value: ContractStatus, failure: Failure | None = None) -> Case[ContractStatus]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_contract_defect, '__module__', "dex_connectors.deel.deelmock")

def get_contract_retry(failure: Failure, after_seconds: float = 0) -> Case[ContractStatus]:
    return Case.retry(failure, after_seconds)

setattr(get_contract_retry, '__module__', "dex_connectors.deel.deelmock")

def get_contract_found_default() -> Case[ContractStatus]:
    return Case.branch_result(FOUND, ContractStatus(id='123e4567-e89b-12d3-a456-426614174000', type='ongoing_time_based', status='in_progress'))

setattr(get_contract_found_default, '__module__', "dex_connectors.deel.deelmock")


class Mock:

    def __init__(self, connection_name: str = "deel") -> None:
        self.get_contract_script: Query[GetContractInput, ContractStatus] = Query(GET_CONTRACT_DEFINITION)
        self.get_contract_script.default(get_contract_found_default())
        self._connection = new_connection_with_operations(
            get_contract=self.get_contract_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.deel.deelmock")

__all__ = [
    "Mock",
    "get_contract_found",
    "get_contract_not_found",
    "get_contract_provider_rejected",
    "get_contract_invalid_response",
    "get_contract_defect",
    "get_contract_retry",
    "get_contract_found_default",
]
