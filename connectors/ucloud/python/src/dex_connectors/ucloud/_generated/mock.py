# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetRegionInput, GetRegionOutput, RegionInfo
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_REGION_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_region_found(value: GetRegionOutput, failure: Failure | None = None) -> Case[GetRegionOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_region_found, '__module__', "dex_connectors.ucloud.ucloudmock")

def get_region_provider_rejected(value: GetRegionOutput, failure: Failure | None = None) -> Case[GetRegionOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_region_provider_rejected, '__module__', "dex_connectors.ucloud.ucloudmock")

def get_region_invalid_response(value: GetRegionOutput, failure: Failure | None = None) -> Case[GetRegionOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_region_invalid_response, '__module__', "dex_connectors.ucloud.ucloudmock")

def get_region_defect(value: GetRegionOutput, failure: Failure | None = None) -> Case[GetRegionOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_region_defect, '__module__', "dex_connectors.ucloud.ucloudmock")

def get_region_retry(failure: Failure, after_seconds: float = 0) -> Case[GetRegionOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_region_retry, '__module__', "dex_connectors.ucloud.ucloudmock")

def get_region_one_region() -> Case[GetRegionOutput]:
    return Case.branch_result(FOUND, GetRegionOutput(regions=(RegionInfo(region_id=1, region_name='Beijing', is_default=True, bit_maps='1', region='cn-bj', zone='cn-bj-01'),)))

setattr(get_region_one_region, '__module__', "dex_connectors.ucloud.ucloudmock")


class Mock:

    def __init__(self, connection_name: str = "ucloud") -> None:
        self.get_region_script: Query[GetRegionInput, GetRegionOutput] = Query(GET_REGION_DEFINITION)
        self.get_region_script.default(get_region_one_region())
        self._connection = new_connection_with_operations(
            get_region=self.get_region_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.ucloud.ucloudmock")

__all__ = [
    "Mock",
    "get_region_found",
    "get_region_provider_rejected",
    "get_region_invalid_response",
    "get_region_defect",
    "get_region_retry",
    "get_region_one_region",
]
