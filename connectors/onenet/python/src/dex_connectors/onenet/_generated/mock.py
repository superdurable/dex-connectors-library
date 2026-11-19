# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DeviceDetail, QueryDeviceDetailInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, PROVIDER_REJECTED, QUERY_DEVICE_DETAIL_DEFINITION

def query_device_detail_found(value: DeviceDetail, failure: Failure | None = None) -> Case[DeviceDetail]:
    return Case.branch_result(FOUND, value, failure)

setattr(query_device_detail_found, '__module__', "dex_connectors.onenet.onenetmock")

def query_device_detail_provider_rejected(value: DeviceDetail, failure: Failure | None = None) -> Case[DeviceDetail]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_device_detail_provider_rejected, '__module__', "dex_connectors.onenet.onenetmock")

def query_device_detail_invalid_response(value: DeviceDetail, failure: Failure | None = None) -> Case[DeviceDetail]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_device_detail_invalid_response, '__module__', "dex_connectors.onenet.onenetmock")

def query_device_detail_defect(value: DeviceDetail, failure: Failure | None = None) -> Case[DeviceDetail]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_device_detail_defect, '__module__', "dex_connectors.onenet.onenetmock")

def query_device_detail_retry(failure: Failure, after_seconds: float = 0) -> Case[DeviceDetail]:
    return Case.retry(failure, after_seconds)

setattr(query_device_detail_retry, '__module__', "dex_connectors.onenet.onenetmock")

def query_device_detail_found_default() -> Case[DeviceDetail]:
    return Case.branch_result(FOUND, DeviceDetail(product_id='demo123', device_name='sensor-1', status=1, description='Sample sensor', create_time='2025-11-28T09:49:00+08:00', last_online_time='2025-12-01T15:38:51+08:00'))

setattr(query_device_detail_found_default, '__module__', "dex_connectors.onenet.onenetmock")


class Mock:

    def __init__(self, connection_name: str = "onenet") -> None:
        self.query_device_detail_script: Query[QueryDeviceDetailInput, DeviceDetail] = Query(QUERY_DEVICE_DETAIL_DEFINITION)
        self.query_device_detail_script.default(query_device_detail_found_default())
        self._connection = new_connection_with_operations(
            query_device_detail=self.query_device_detail_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.onenet.onenetmock")

__all__ = [
    "Mock",
    "query_device_detail_found",
    "query_device_detail_provider_rejected",
    "query_device_detail_invalid_response",
    "query_device_detail_defect",
    "query_device_detail_retry",
    "query_device_detail_found_default",
]
