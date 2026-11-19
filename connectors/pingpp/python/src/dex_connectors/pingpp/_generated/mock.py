# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetChargeStatusInput, GetChargeStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, FETCHED, GET_CHARGE_STATUS_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_charge_status_fetched(value: GetChargeStatusOutput, failure: Failure | None = None) -> Case[GetChargeStatusOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(get_charge_status_fetched, '__module__', "dex_connectors.pingpp.pingppmock")

def get_charge_status_not_found(value: GetChargeStatusOutput, failure: Failure | None = None) -> Case[GetChargeStatusOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_charge_status_not_found, '__module__', "dex_connectors.pingpp.pingppmock")

def get_charge_status_provider_rejected(value: GetChargeStatusOutput, failure: Failure | None = None) -> Case[GetChargeStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_charge_status_provider_rejected, '__module__', "dex_connectors.pingpp.pingppmock")

def get_charge_status_invalid_response(value: GetChargeStatusOutput, failure: Failure | None = None) -> Case[GetChargeStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_charge_status_invalid_response, '__module__', "dex_connectors.pingpp.pingppmock")

def get_charge_status_defect(value: GetChargeStatusOutput, failure: Failure | None = None) -> Case[GetChargeStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_charge_status_defect, '__module__', "dex_connectors.pingpp.pingppmock")

def get_charge_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetChargeStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_charge_status_retry, '__module__', "dex_connectors.pingpp.pingppmock")

def get_charge_status_sample_charge() -> Case[GetChargeStatusOutput]:
    return Case.branch_result(FETCHED, GetChargeStatusOutput(charge_id='ch_sample123', paid=False, refunded=False, reversed=False, live_mode=False))

setattr(get_charge_status_sample_charge, '__module__', "dex_connectors.pingpp.pingppmock")


class Mock:

    def __init__(self, connection_name: str = "pingpp") -> None:
        self.get_charge_status_script: Query[GetChargeStatusInput, GetChargeStatusOutput] = Query(GET_CHARGE_STATUS_DEFINITION)
        self.get_charge_status_script.default(get_charge_status_sample_charge())
        self._connection = new_connection_with_operations(
            get_charge_status=self.get_charge_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.pingpp.pingppmock")

__all__ = [
    "Mock",
    "get_charge_status_fetched",
    "get_charge_status_not_found",
    "get_charge_status_provider_rejected",
    "get_charge_status_invalid_response",
    "get_charge_status_defect",
    "get_charge_status_retry",
    "get_charge_status_sample_charge",
]
