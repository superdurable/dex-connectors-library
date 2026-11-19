# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetInstanceOnlineStatusInput, GetInstanceOnlineStatusOutput
from .definitions import CONNECTOR_ID, CHECKED, DEFECT, GET_INSTANCE_ONLINE_STATUS_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_instance_online_status_checked(value: GetInstanceOnlineStatusOutput, failure: Failure | None = None) -> Case[GetInstanceOnlineStatusOutput]:
    return Case.branch_result(CHECKED, value, failure)

setattr(get_instance_online_status_checked, '__module__', "dex_connectors.xingyapi.xingyapimock")

def get_instance_online_status_provider_rejected(value: GetInstanceOnlineStatusOutput, failure: Failure | None = None) -> Case[GetInstanceOnlineStatusOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_instance_online_status_provider_rejected, '__module__', "dex_connectors.xingyapi.xingyapimock")

def get_instance_online_status_invalid_response(value: GetInstanceOnlineStatusOutput, failure: Failure | None = None) -> Case[GetInstanceOnlineStatusOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_instance_online_status_invalid_response, '__module__', "dex_connectors.xingyapi.xingyapimock")

def get_instance_online_status_defect(value: GetInstanceOnlineStatusOutput, failure: Failure | None = None) -> Case[GetInstanceOnlineStatusOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_instance_online_status_defect, '__module__', "dex_connectors.xingyapi.xingyapimock")

def get_instance_online_status_retry(failure: Failure, after_seconds: float = 0) -> Case[GetInstanceOnlineStatusOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_instance_online_status_retry, '__module__', "dex_connectors.xingyapi.xingyapimock")

def get_instance_online_status_online_instance() -> Case[GetInstanceOnlineStatusOutput]:
    return Case.branch_result(CHECKED, GetInstanceOnlineStatusOutput(is_online=True, account_id='1688850000000001'))

setattr(get_instance_online_status_online_instance, '__module__', "dex_connectors.xingyapi.xingyapimock")

def get_instance_online_status_offline_instance() -> Case[GetInstanceOnlineStatusOutput]:
    return Case.branch_result(CHECKED, GetInstanceOnlineStatusOutput(is_online=False, account_id=''))

setattr(get_instance_online_status_offline_instance, '__module__', "dex_connectors.xingyapi.xingyapimock")


class Mock:

    def __init__(self, connection_name: str = "xingyapi") -> None:
        self.get_instance_online_status_script: Query[GetInstanceOnlineStatusInput, GetInstanceOnlineStatusOutput] = Query(GET_INSTANCE_ONLINE_STATUS_DEFINITION)
        self.get_instance_online_status_script.default(get_instance_online_status_online_instance())
        self._connection = new_connection_with_operations(
            get_instance_online_status=self.get_instance_online_status_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.xingyapi.xingyapimock")

__all__ = [
    "Mock",
    "get_instance_online_status_checked",
    "get_instance_online_status_provider_rejected",
    "get_instance_online_status_invalid_response",
    "get_instance_online_status_defect",
    "get_instance_online_status_retry",
    "get_instance_online_status_online_instance",
    "get_instance_online_status_offline_instance",
]
