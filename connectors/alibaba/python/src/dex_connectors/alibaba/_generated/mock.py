# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DescribeRegionsInput, DescribeRegionsOutput, ECSRegion
from .definitions import CONNECTOR_ID, DEFECT, DESCRIBE_REGIONS_DEFINITION, FETCHED, INVALID_RESPONSE, PROVIDER_REJECTED

def describe_regions_fetched(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(FETCHED, value, failure)

setattr(describe_regions_fetched, '__module__', "dex_connectors.alibaba.alibabamock")

def describe_regions_provider_rejected(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(describe_regions_provider_rejected, '__module__', "dex_connectors.alibaba.alibabamock")

def describe_regions_invalid_response(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(describe_regions_invalid_response, '__module__', "dex_connectors.alibaba.alibabamock")

def describe_regions_defect(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(describe_regions_defect, '__module__', "dex_connectors.alibaba.alibabamock")

def describe_regions_retry(failure: Failure, after_seconds: float = 0) -> Case[DescribeRegionsOutput]:
    return Case.retry(failure, after_seconds)

setattr(describe_regions_retry, '__module__', "dex_connectors.alibaba.alibabamock")

def describe_regions_sample_regions() -> Case[DescribeRegionsOutput]:
    return Case.branch_result(FETCHED, DescribeRegionsOutput(regions=(ECSRegion(region_id='cn-hangzhou', local_name='华东1（杭州）', endpoint='ecs.cn-hangzhou.aliyuncs.com', status='available'),)))

setattr(describe_regions_sample_regions, '__module__', "dex_connectors.alibaba.alibabamock")


class Mock:

    def __init__(self, connection_name: str = "alibaba") -> None:
        self.describe_regions_script: Query[DescribeRegionsInput, DescribeRegionsOutput] = Query(DESCRIBE_REGIONS_DEFINITION)
        self.describe_regions_script.default(describe_regions_sample_regions())
        self._connection = new_connection_with_operations(
            describe_regions=self.describe_regions_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.alibaba.alibabamock")

__all__ = [
    "Mock",
    "describe_regions_fetched",
    "describe_regions_provider_rejected",
    "describe_regions_invalid_response",
    "describe_regions_defect",
    "describe_regions_retry",
    "describe_regions_sample_regions",
]
