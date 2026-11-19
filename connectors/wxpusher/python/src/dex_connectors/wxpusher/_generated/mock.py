# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSubscriberCountInput, SubscriberCountResult
from .definitions import CONNECTOR_ID, COUNTED, DEFECT, GET_SUBSCRIBER_COUNT_DEFINITION, INVALID_RESPONSE, PROVIDER_REJECTED

def get_subscriber_count_counted(value: SubscriberCountResult, failure: Failure | None = None) -> Case[SubscriberCountResult]:
    return Case.branch_result(COUNTED, value, failure)

setattr(get_subscriber_count_counted, '__module__', "dex_connectors.wxpusher.wxpushermock")

def get_subscriber_count_provider_rejected(value: SubscriberCountResult, failure: Failure | None = None) -> Case[SubscriberCountResult]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_subscriber_count_provider_rejected, '__module__', "dex_connectors.wxpusher.wxpushermock")

def get_subscriber_count_invalid_response(value: SubscriberCountResult, failure: Failure | None = None) -> Case[SubscriberCountResult]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_subscriber_count_invalid_response, '__module__', "dex_connectors.wxpusher.wxpushermock")

def get_subscriber_count_defect(value: SubscriberCountResult, failure: Failure | None = None) -> Case[SubscriberCountResult]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_subscriber_count_defect, '__module__', "dex_connectors.wxpusher.wxpushermock")

def get_subscriber_count_retry(failure: Failure, after_seconds: float = 0) -> Case[SubscriberCountResult]:
    return Case.retry(failure, after_seconds)

setattr(get_subscriber_count_retry, '__module__', "dex_connectors.wxpusher.wxpushermock")

def get_subscriber_count_counted_subscribers() -> Case[SubscriberCountResult]:
    return Case.branch_result(COUNTED, SubscriberCountResult(total=40))

setattr(get_subscriber_count_counted_subscribers, '__module__', "dex_connectors.wxpusher.wxpushermock")


class Mock:

    def __init__(self, connection_name: str = "wxpusher") -> None:
        self.get_subscriber_count_script: Query[GetSubscriberCountInput, SubscriberCountResult] = Query(GET_SUBSCRIBER_COUNT_DEFINITION)
        self.get_subscriber_count_script.default(get_subscriber_count_counted_subscribers())
        self._connection = new_connection_with_operations(
            get_subscriber_count=self.get_subscriber_count_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.wxpusher.wxpushermock")

__all__ = [
    "Mock",
    "get_subscriber_count_counted",
    "get_subscriber_count_provider_rejected",
    "get_subscriber_count_invalid_response",
    "get_subscriber_count_defect",
    "get_subscriber_count_retry",
    "get_subscriber_count_counted_subscribers",
]
