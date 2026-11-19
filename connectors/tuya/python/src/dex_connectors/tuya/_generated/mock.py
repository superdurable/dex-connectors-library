# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetDeviceDetailsInput, GetDeviceDetailsOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_DEVICE_DETAILS_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_device_details_found(value: GetDeviceDetailsOutput, failure: Failure | None = None) -> Case[GetDeviceDetailsOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_device_details_found, '__module__', "dex_connectors.tuya.tuyamock")

def get_device_details_not_found(value: GetDeviceDetailsOutput, failure: Failure | None = None) -> Case[GetDeviceDetailsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_device_details_not_found, '__module__', "dex_connectors.tuya.tuyamock")

def get_device_details_provider_rejected(value: GetDeviceDetailsOutput, failure: Failure | None = None) -> Case[GetDeviceDetailsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_device_details_provider_rejected, '__module__', "dex_connectors.tuya.tuyamock")

def get_device_details_invalid_response(value: GetDeviceDetailsOutput, failure: Failure | None = None) -> Case[GetDeviceDetailsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_device_details_invalid_response, '__module__', "dex_connectors.tuya.tuyamock")

def get_device_details_defect(value: GetDeviceDetailsOutput, failure: Failure | None = None) -> Case[GetDeviceDetailsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_device_details_defect, '__module__', "dex_connectors.tuya.tuyamock")

def get_device_details_retry(failure: Failure, after_seconds: float = 0) -> Case[GetDeviceDetailsOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_device_details_retry, '__module__', "dex_connectors.tuya.tuyamock")

def get_device_details_linked_device() -> Case[GetDeviceDetailsOutput]:
    return Case.branch_result(FOUND, GetDeviceDetailsOutput(device_id='60613135b121cddc294a', name='Body fat scale', online=True, category='qt', product_id='g0er6hSKgMqr', created_at_unix=1560827137, updated_at_unix=1589764585))

setattr(get_device_details_linked_device, '__module__', "dex_connectors.tuya.tuyamock")


class Mock:

    def __init__(self, connection_name: str = "tuya") -> None:
        self.get_device_details_script: Query[GetDeviceDetailsInput, GetDeviceDetailsOutput] = Query(GET_DEVICE_DETAILS_DEFINITION)
        self.get_device_details_script.default(get_device_details_linked_device())
        self._connection = new_connection_with_operations(
            get_device_details=self.get_device_details_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.tuya.tuyamock")

__all__ = [
    "Mock",
    "get_device_details_found",
    "get_device_details_not_found",
    "get_device_details_provider_rejected",
    "get_device_details_invalid_response",
    "get_device_details_defect",
    "get_device_details_retry",
    "get_device_details_linked_device",
]
