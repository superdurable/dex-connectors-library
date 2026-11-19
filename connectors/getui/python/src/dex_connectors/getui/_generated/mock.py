# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetDailyPushReportInput, GetDailyPushReportOutput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_DAILY_PUSH_REPORT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_daily_push_report_found(value: GetDailyPushReportOutput, failure: Failure | None = None) -> Case[GetDailyPushReportOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_daily_push_report_found, '__module__', "dex_connectors.getui.getuimock")

def get_daily_push_report_provider_rejected(value: GetDailyPushReportOutput, failure: Failure | None = None) -> Case[GetDailyPushReportOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_daily_push_report_provider_rejected, '__module__', "dex_connectors.getui.getuimock")

def get_daily_push_report_invalid_response(value: GetDailyPushReportOutput, failure: Failure | None = None) -> Case[GetDailyPushReportOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_daily_push_report_invalid_response, '__module__', "dex_connectors.getui.getuimock")

def get_daily_push_report_defect(value: GetDailyPushReportOutput, failure: Failure | None = None) -> Case[GetDailyPushReportOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_daily_push_report_defect, '__module__', "dex_connectors.getui.getuimock")

def get_daily_push_report_retry(failure: Failure, after_seconds: float = 0) -> Case[GetDailyPushReportOutput]:
    return Case.retry(failure, after_seconds)

setattr(get_daily_push_report_retry, '__module__', "dex_connectors.getui.getuimock")

def get_daily_push_report_daily_totals() -> Case[GetDailyPushReportOutput]:
    return Case.branch_result(FOUND, GetDailyPushReportOutput(date='2021-01-01', request_count=10, eligible_message_count=4, sent_count=4, received_count=4, displayed_count=2, clicked_count=2))

setattr(get_daily_push_report_daily_totals, '__module__', "dex_connectors.getui.getuimock")


class Mock:

    def __init__(self, connection_name: str = "getui") -> None:
        self.get_daily_push_report_script: Query[GetDailyPushReportInput, GetDailyPushReportOutput] = Query(GET_DAILY_PUSH_REPORT_DEFINITION)
        self.get_daily_push_report_script.default(get_daily_push_report_daily_totals())
        self._connection = new_connection_with_operations(
            get_daily_push_report=self.get_daily_push_report_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.getui.getuimock")

__all__ = [
    "Mock",
    "get_daily_push_report_found",
    "get_daily_push_report_provider_rejected",
    "get_daily_push_report_invalid_response",
    "get_daily_push_report_defect",
    "get_daily_push_report_retry",
    "get_daily_push_report_daily_totals",
]
