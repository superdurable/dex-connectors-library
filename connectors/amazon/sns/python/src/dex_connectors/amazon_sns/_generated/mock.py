# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTopicsInput, TopicPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LIST_TOPICS_DEFINITION, PAGE, PROVIDER_REJECTED

def list_topics_page(value: TopicPage, failure: Failure | None = None) -> Case[TopicPage]:
    return Case.branch_result(PAGE, value, failure)

setattr(list_topics_page, '__module__', "dex_connectors.amazon_sns.snsmock")

def list_topics_provider_rejected(value: TopicPage, failure: Failure | None = None) -> Case[TopicPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_topics_provider_rejected, '__module__', "dex_connectors.amazon_sns.snsmock")

def list_topics_invalid_response(value: TopicPage, failure: Failure | None = None) -> Case[TopicPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_topics_invalid_response, '__module__', "dex_connectors.amazon_sns.snsmock")

def list_topics_defect(value: TopicPage, failure: Failure | None = None) -> Case[TopicPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_topics_defect, '__module__', "dex_connectors.amazon_sns.snsmock")

def list_topics_retry(failure: Failure, after_seconds: float = 0) -> Case[TopicPage]:
    return Case.retry(failure, after_seconds)

setattr(list_topics_retry, '__module__', "dex_connectors.amazon_sns.snsmock")

def list_topics_page_default() -> Case[TopicPage]:
    return Case.branch_result(PAGE, TopicPage(topic_ar_ns=('arn:aws:sns:us-east-1:123456789012:example-topic',), next_token=''))

setattr(list_topics_page_default, '__module__', "dex_connectors.amazon_sns.snsmock")


class Mock:

    def __init__(self, connection_name: str = "amazon-sns") -> None:
        self.list_topics_script: Query[ListTopicsInput, TopicPage] = Query(LIST_TOPICS_DEFINITION)
        self.list_topics_script.default(list_topics_page_default())
        self._connection = new_connection_with_operations(
            list_topics=self.list_topics_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.amazon_sns.snsmock")

__all__ = [
    "Mock",
    "list_topics_page",
    "list_topics_provider_rejected",
    "list_topics_invalid_response",
    "list_topics_defect",
    "list_topics_retry",
    "list_topics_page_default",
]
