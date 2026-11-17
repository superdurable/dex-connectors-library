# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import Device, DevicePage, ListDevicesInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_DEVICES_DEFINITION, PROVIDER_REJECTED

def list_devices_listed(value: DevicePage, failure: Failure | None = None) -> Case[DevicePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_devices_listed, '__module__', "dex_connectors.aqara.aqaramock")

def list_devices_provider_rejected(value: DevicePage, failure: Failure | None = None) -> Case[DevicePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_devices_provider_rejected, '__module__', "dex_connectors.aqara.aqaramock")

def list_devices_invalid_response(value: DevicePage, failure: Failure | None = None) -> Case[DevicePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_devices_invalid_response, '__module__', "dex_connectors.aqara.aqaramock")

def list_devices_defect(value: DevicePage, failure: Failure | None = None) -> Case[DevicePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_devices_defect, '__module__', "dex_connectors.aqara.aqaramock")

def list_devices_retry(failure: Failure, after_seconds: float = 0) -> Case[DevicePage]:
    return Case.retry(failure, after_seconds)

setattr(list_devices_retry, '__module__', "dex_connectors.aqara.aqaramock")

def list_devices_listed_devices() -> Case[DevicePage]:
    return Case.branch_result(LISTED, DevicePage(total_count=1, page_num=1, page_size=50, devices=(Device(device_id='lumi.07737309957642', device_name='Aqara Hub', model='lumi.gateway.aqhm01', model_type=1, online=True, firmware_version='3.2.6', position_id='real2.730432352746111072', parent_device_id='', time_zone='GMT+08:00'),)))

setattr(list_devices_listed_devices, '__module__', "dex_connectors.aqara.aqaramock")


class Mock:

    def __init__(self, connection_name: str = "aqara") -> None:
        self.list_devices_script: Query[ListDevicesInput, DevicePage] = Query(LIST_DEVICES_DEFINITION)
        self.list_devices_script.default(list_devices_listed_devices())
        self._connection = new_connection_with_operations(
            list_devices=self.list_devices_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.aqara.aqaramock")

__all__ = [
    "Mock",
    "list_devices_listed",
    "list_devices_provider_rejected",
    "list_devices_invalid_response",
    "list_devices_defect",
    "list_devices_retry",
    "list_devices_listed_devices",
]
