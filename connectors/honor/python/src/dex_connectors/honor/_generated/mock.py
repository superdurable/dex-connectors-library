# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import LookupAppIDInput, LookupAppIDOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LOOKUP_APP_ID_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def lookup_app_id_found(value: LookupAppIDOutput, failure: Failure | None = None) -> Case[LookupAppIDOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(lookup_app_id_found, '__module__', "dex_connectors.honor.honormock")

def lookup_app_id_not_found(value: LookupAppIDOutput, failure: Failure | None = None) -> Case[LookupAppIDOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(lookup_app_id_not_found, '__module__', "dex_connectors.honor.honormock")

def lookup_app_id_provider_rejected(value: LookupAppIDOutput, failure: Failure | None = None) -> Case[LookupAppIDOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(lookup_app_id_provider_rejected, '__module__', "dex_connectors.honor.honormock")

def lookup_app_id_invalid_response(value: LookupAppIDOutput, failure: Failure | None = None) -> Case[LookupAppIDOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(lookup_app_id_invalid_response, '__module__', "dex_connectors.honor.honormock")

def lookup_app_id_defect(value: LookupAppIDOutput, failure: Failure | None = None) -> Case[LookupAppIDOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(lookup_app_id_defect, '__module__', "dex_connectors.honor.honormock")

def lookup_app_id_retry(failure: Failure, after_seconds: float = 0) -> Case[LookupAppIDOutput]:
    return Case.retry(failure, after_seconds)

setattr(lookup_app_id_retry, '__module__', "dex_connectors.honor.honormock")

def lookup_app_id_found_app() -> Case[LookupAppIDOutput]:
    return Case.branch_result(FOUND, LookupAppIDOutput(package_name='com.example.app', app_id=123456))

setattr(lookup_app_id_found_app, '__module__', "dex_connectors.honor.honormock")

def lookup_app_id_no_app() -> Case[LookupAppIDOutput]:
    return Case.branch_result(NOT_FOUND, LookupAppIDOutput(package_name='com.example.app', app_id=0))

setattr(lookup_app_id_no_app, '__module__', "dex_connectors.honor.honormock")


class Mock:

    def __init__(self, connection_name: str = "honor") -> None:
        self.lookup_app_id_script: Query[LookupAppIDInput, LookupAppIDOutput] = Query(LOOKUP_APP_ID_DEFINITION)
        self.lookup_app_id_script.default(lookup_app_id_found_app())
        self._connection = new_connection_with_operations(
            lookup_app_id=self.lookup_app_id_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.honor.honormock")

__all__ = [
    "Mock",
    "lookup_app_id_found",
    "lookup_app_id_not_found",
    "lookup_app_id_provider_rejected",
    "lookup_app_id_invalid_response",
    "lookup_app_id_defect",
    "lookup_app_id_retry",
    "lookup_app_id_found_app",
    "lookup_app_id_no_app",
]
