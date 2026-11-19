# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListIndustriesInput, ListIndustriesOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_INDUSTRIES_DEFINITION, PROVIDER_REJECTED

def list_industries_found(value: ListIndustriesOutput, failure: Failure | None = None) -> Case[ListIndustriesOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_industries_found, '__module__', "dex_connectors.shixiseng.shixisengmock")

def list_industries_provider_rejected(value: ListIndustriesOutput, failure: Failure | None = None) -> Case[ListIndustriesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_industries_provider_rejected, '__module__', "dex_connectors.shixiseng.shixisengmock")

def list_industries_invalid_response(value: ListIndustriesOutput, failure: Failure | None = None) -> Case[ListIndustriesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_industries_invalid_response, '__module__', "dex_connectors.shixiseng.shixisengmock")

def list_industries_defect(value: ListIndustriesOutput, failure: Failure | None = None) -> Case[ListIndustriesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_industries_defect, '__module__', "dex_connectors.shixiseng.shixisengmock")

def list_industries_retry(failure: Failure, after_seconds: float = 0) -> Case[ListIndustriesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_industries_retry, '__module__', "dex_connectors.shixiseng.shixisengmock")

def list_industries_industries() -> Case[ListIndustriesOutput]:
    return Case.branch_result(FOUND, ListIndustriesOutput(industries=('互联网/游戏/软件', '教育/培训')))

setattr(list_industries_industries, '__module__', "dex_connectors.shixiseng.shixisengmock")


class Mock:

    def __init__(self, connection_name: str = "shixiseng") -> None:
        self.list_industries_script: Query[ListIndustriesInput, ListIndustriesOutput] = Query(LIST_INDUSTRIES_DEFINITION)
        self.list_industries_script.default(list_industries_industries())
        self._connection = new_connection_with_operations(
            list_industries=self.list_industries_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.shixiseng.shixisengmock")

__all__ = [
    "Mock",
    "list_industries_found",
    "list_industries_provider_rejected",
    "list_industries_invalid_response",
    "list_industries_defect",
    "list_industries_retry",
    "list_industries_industries",
]
