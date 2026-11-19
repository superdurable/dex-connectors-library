# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetServiceUsageInput, ServiceUsage
from .definitions import CONNECTOR_ID, DEFECT, GET_SERVICE_USAGE_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED, READ

def get_service_usage_read(value: ServiceUsage, failure: Failure | None = None) -> Case[ServiceUsage]:
    return Case.branch_result(READ, value, failure)

setattr(get_service_usage_read, '__module__', "dex_connectors.upyun.upyunmock")

def get_service_usage_provider_rejected(value: ServiceUsage, failure: Failure | None = None) -> Case[ServiceUsage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_service_usage_provider_rejected, '__module__', "dex_connectors.upyun.upyunmock")

def get_service_usage_invalid_response(value: ServiceUsage, failure: Failure | None = None) -> Case[ServiceUsage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_service_usage_invalid_response, '__module__', "dex_connectors.upyun.upyunmock")

def get_service_usage_defect(value: ServiceUsage, failure: Failure | None = None) -> Case[ServiceUsage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_service_usage_defect, '__module__', "dex_connectors.upyun.upyunmock")

def get_service_usage_retry(failure: Failure, after_seconds: float = 0) -> Case[ServiceUsage]:
    return Case.retry(failure, after_seconds)

setattr(get_service_usage_retry, '__module__', "dex_connectors.upyun.upyunmock")

def get_service_usage_read_default() -> Case[ServiceUsage]:
    return Case.branch_result(READ, ServiceUsage(bytes=1024))

setattr(get_service_usage_read_default, '__module__', "dex_connectors.upyun.upyunmock")


class Mock:

    def __init__(self, connection_name: str = "upyun") -> None:
        self.get_service_usage_script: Query[GetServiceUsageInput, ServiceUsage] = Query(GET_SERVICE_USAGE_DEFINITION)
        self.get_service_usage_script.default(get_service_usage_read_default())
        self._connection = new_connection_with_operations(
            get_service_usage=self.get_service_usage_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.upyun.upyunmock")

__all__ = [
    "Mock",
    "get_service_usage_read",
    "get_service_usage_provider_rejected",
    "get_service_usage_invalid_response",
    "get_service_usage_defect",
    "get_service_usage_retry",
    "get_service_usage_read_default",
]
