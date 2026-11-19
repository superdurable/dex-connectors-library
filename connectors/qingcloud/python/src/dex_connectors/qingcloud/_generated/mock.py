# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DescribeZonesInput, Zone, ZonesPage
from .definitions import CONNECTOR_ID, DEFECT, DESCRIBE_ZONES_DEFINITION, INVALID_RESPONSE, LISTED, PROVIDER_REJECTED

def describe_zones_listed(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(describe_zones_listed, '__module__', "dex_connectors.qingcloud.qingcloudmock")

def describe_zones_provider_rejected(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(describe_zones_provider_rejected, '__module__', "dex_connectors.qingcloud.qingcloudmock")

def describe_zones_invalid_response(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(describe_zones_invalid_response, '__module__', "dex_connectors.qingcloud.qingcloudmock")

def describe_zones_defect(value: ZonesPage, failure: Failure | None = None) -> Case[ZonesPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(describe_zones_defect, '__module__', "dex_connectors.qingcloud.qingcloudmock")

def describe_zones_retry(failure: Failure, after_seconds: float = 0) -> Case[ZonesPage]:
    return Case.retry(failure, after_seconds)

setattr(describe_zones_retry, '__module__', "dex_connectors.qingcloud.qingcloudmock")

def describe_zones_listed_default() -> Case[ZonesPage]:
    return Case.branch_result(LISTED, ZonesPage(zones=(Zone(zone_id='pek3a', status='active'),), total_count=1))

setattr(describe_zones_listed_default, '__module__', "dex_connectors.qingcloud.qingcloudmock")


class Mock:

    def __init__(self, connection_name: str = "qingcloud") -> None:
        self.describe_zones_script: Query[DescribeZonesInput, ZonesPage] = Query(DESCRIBE_ZONES_DEFINITION)
        self.describe_zones_script.default(describe_zones_listed_default())
        self._connection = new_connection_with_operations(
            describe_zones=self.describe_zones_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.qingcloud.qingcloudmock")

__all__ = [
    "Mock",
    "describe_zones_listed",
    "describe_zones_provider_rejected",
    "describe_zones_invalid_response",
    "describe_zones_defect",
    "describe_zones_retry",
    "describe_zones_listed_default",
]
