# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetBehaviorSummaryInput, GetBehaviorSummaryOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_BEHAVIOR_SUMMARY_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_behavior_summary_found(value: GetBehaviorSummaryOutput, failure: Failure | None = None) -> Case[GetBehaviorSummaryOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_behavior_summary_found, '__module__', "dex_connectors.douyin.douyinmock")

def get_behavior_summary_provider_rejected(value: GetBehaviorSummaryOutput, failure: Failure | None = None) -> Case[GetBehaviorSummaryOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_behavior_summary_provider_rejected, '__module__', "dex_connectors.douyin.douyinmock")

def get_behavior_summary_invalid_response(value: GetBehaviorSummaryOutput, failure: Failure | None = None) -> Case[GetBehaviorSummaryOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_behavior_summary_invalid_response, '__module__', "dex_connectors.douyin.douyinmock")

def get_behavior_summary_defect(value: GetBehaviorSummaryOutput, failure: Failure | None = None) -> Case[GetBehaviorSummaryOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_behavior_summary_defect, '__module__', "dex_connectors.douyin.douyinmock")

def get_behavior_summary_retry(failure: Failure, after_seconds: float = 0) -> Case[GetBehaviorSummaryOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_behavior_summary_retry, '__module__', "dex_connectors.douyin.douyinmock")

def get_behavior_summary_summary() -> Case[GetBehaviorSummaryOutput]:
    return Case.branch_result(FOUND, GetBehaviorSummaryOutput(active_user_count=113, new_user_count=74, total_user_count=285227, open_count=190, share_count=8))

setattr(get_behavior_summary_summary, '__module__', "dex_connectors.douyin.douyinmock")


class Mock:

    def __init__(self, connection_name: str = "douyin") -> None:
        self.get_behavior_summary_script: Query[GetBehaviorSummaryInput, GetBehaviorSummaryOutput] = Query(GET_BEHAVIOR_SUMMARY_DEFINITION)
        self.get_behavior_summary_script.default(get_behavior_summary_summary())
        self._connection = new_connection_with_operations(
            get_behavior_summary=self.get_behavior_summary_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.douyin.douyinmock")

__all__ = [
    "Mock",
    "get_behavior_summary_found",
    "get_behavior_summary_provider_rejected",
    "get_behavior_summary_invalid_response",
    "get_behavior_summary_defect",
    "get_behavior_summary_retry",
    "get_behavior_summary_summary",
]
