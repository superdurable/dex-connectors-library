# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import DescribeRegionsInput, DescribeRegionsOutput
from .definitions import CONNECTOR_ID, DEFECT, DESCRIBE_REGIONS_DEFINITION, INVALID_RESPONSE, LISTED, PROVIDER_REJECTED

def describe_regions_listed(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(describe_regions_listed, '__module__', "dex_connectors.volcengine.volcenginemock")

def describe_regions_provider_rejected(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(describe_regions_provider_rejected, '__module__', "dex_connectors.volcengine.volcenginemock")

def describe_regions_invalid_response(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(describe_regions_invalid_response, '__module__', "dex_connectors.volcengine.volcenginemock")

def describe_regions_defect(value: DescribeRegionsOutput, failure: Failure | None = None) -> Case[DescribeRegionsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(describe_regions_defect, '__module__', "dex_connectors.volcengine.volcenginemock")

def describe_regions_retry(failure: Failure, after_seconds: float = 0) -> Case[DescribeRegionsOutput]:
    return Case.retry(failure, after_seconds)

setattr(describe_regions_retry, '__module__', "dex_connectors.volcengine.volcenginemock")

def describe_regions_sample_region_page() -> Case[DescribeRegionsOutput]:
    return Case.branch_result(LISTED, DescribeRegionsOutput(region_ids=('cn-beijing', 'cn-shanghai'), next_token=''))

setattr(describe_regions_sample_region_page, '__module__', "dex_connectors.volcengine.volcenginemock")


class Mock:

    def __init__(self, connection_name: str = "volcengine") -> None:
        self.describe_regions_script: Query[DescribeRegionsInput, DescribeRegionsOutput] = Query(DESCRIBE_REGIONS_DEFINITION)
        self.describe_regions_script.default(describe_regions_sample_region_page())
        self._connection = new_connection_with_operations(
            describe_regions=self.describe_regions_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.volcengine.volcenginemock")

__all__ = [
    "Mock",
    "describe_regions_listed",
    "describe_regions_provider_rejected",
    "describe_regions_invalid_response",
    "describe_regions_defect",
    "describe_regions_retry",
    "describe_regions_sample_region_page",
]
