# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Balance, GetBalanceInput
from .definitions import CONNECTOR_ID, DEFECT, GET_BALANCE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, RETRIEVED

def get_balance_retrieved(value: Balance, failure: Failure | None = None) -> Case[Balance]:
    return Case.branch_result(RETRIEVED, value, failure)

setattr(get_balance_retrieved, '__module__', "dex_connectors.tokenfactory.tokenfactorymock")

def get_balance_provider_rejected(value: Balance, failure: Failure | None = None) -> Case[Balance]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_balance_provider_rejected, '__module__', "dex_connectors.tokenfactory.tokenfactorymock")

def get_balance_invalid_response(value: Balance, failure: Failure | None = None) -> Case[Balance]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_balance_invalid_response, '__module__', "dex_connectors.tokenfactory.tokenfactorymock")

def get_balance_defect(value: Balance, failure: Failure | None = None) -> Case[Balance]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_balance_defect, '__module__', "dex_connectors.tokenfactory.tokenfactorymock")

def get_balance_retry(failure: Failure, after_seconds: float = 0) -> Case[Balance]:
    return Case.retry(failure, after_seconds)

setattr(get_balance_retry, '__module__', "dex_connectors.tokenfactory.tokenfactorymock")

def get_balance_retrieved_balance() -> Case[Balance]:
    return Case.branch_result(RETRIEVED, Balance(balance_yuan=12.345678, is_active=True, unit='CNY'))

setattr(get_balance_retrieved_balance, '__module__', "dex_connectors.tokenfactory.tokenfactorymock")


class Mock:

    def __init__(self, connection_name: str = "tokenfactory") -> None:
        self.get_balance_script: Query[GetBalanceInput, Balance] = Query(GET_BALANCE_DEFINITION)
        self.get_balance_script.default(get_balance_retrieved_balance())
        self._connection = new_connection_with_operations(
            get_balance=self.get_balance_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tokenfactory.tokenfactorymock")

__all__ = [
    "Mock",
    "get_balance_retrieved",
    "get_balance_provider_rejected",
    "get_balance_invalid_response",
    "get_balance_defect",
    "get_balance_retry",
    "get_balance_retrieved_balance",
]
