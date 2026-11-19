# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import FlightStatus, GetFlightStatusInput, GetFlightStatusOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_FLIGHT_STATUS_DEFINITION, INVALID_RESPONSE, NO_DATA, PROVIDER_REJECTED

def get_flight_status_found(value: GetFlightStatusOutput, failure: Failure | None = None) -> Case[GetFlightStatusOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_flight_status_found, '__module__', "dex_connectors.variflight.variflightmock")

def get_flight_status_no_data(value: GetFlightStatusOutput, failure: Failure | None = None) -> Case[GetFlightStatusOutput]:
    return Case.branch_result(NO_DATA, value, failure)

setattr(get_flight_status_no_data, '__module__', "dex_connectors.variflight.variflightmock")

def get_flight_status_provider_rejected(value: GetFlightStatusOutput, failure: Failure | None = None) -> Case[GetFlightStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_flight_status_provider_rejected, '__module__', "dex_connectors.variflight.variflightmock")

def get_flight_status_invalid_response(value: GetFlightStatusOutput, failure: Failure | None = None) -> Case[GetFlightStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_flight_status_invalid_response, '__module__', "dex_connectors.variflight.variflightmock")

def get_flight_status_defect(value: GetFlightStatusOutput, failure: Failure | None = None) -> Case[GetFlightStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_flight_status_defect, '__module__', "dex_connectors.variflight.variflightmock")

def get_flight_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetFlightStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_flight_status_retry, '__module__', "dex_connectors.variflight.variflightmock")

def get_flight_status_scheduled_flight() -> Case[GetFlightStatusOutput]:
    return Case.branch_result(FOUND, GetFlightStatusOutput(total=1, flights=(FlightStatus(flight_number='GX8915', state='schedule', state_code=0, scheduled_departure='2026-11-13 08:20:00', actual_departure=''),)))

setattr(get_flight_status_scheduled_flight, '__module__', "dex_connectors.variflight.variflightmock")

def get_flight_status_no_flight() -> Case[GetFlightStatusOutput]:
    return Case.branch_result(NO_DATA, GetFlightStatusOutput(total=0, flights=()))

setattr(get_flight_status_no_flight, '__module__', "dex_connectors.variflight.variflightmock")


class Mock:

    def __init__(self, connection_name: str = "variflight") -> None:
        self.get_flight_status_script: Query[GetFlightStatusInput, GetFlightStatusOutput] = Query(GET_FLIGHT_STATUS_DEFINITION)
        self.get_flight_status_script.default(get_flight_status_scheduled_flight())
        self._connection = new_connection_with_operations(
            get_flight_status=self.get_flight_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.variflight.variflightmock")

__all__ = [
    "Mock",
    "get_flight_status_found",
    "get_flight_status_no_data",
    "get_flight_status_provider_rejected",
    "get_flight_status_invalid_response",
    "get_flight_status_defect",
    "get_flight_status_retry",
    "get_flight_status_scheduled_flight",
    "get_flight_status_no_flight",
]
