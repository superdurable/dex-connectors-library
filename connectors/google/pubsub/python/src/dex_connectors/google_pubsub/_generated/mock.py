# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListTopicsInput, Topic, TopicsPage
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_TOPICS_DEFINITION, PROVIDER_REJECTED

def list_topics_listed(value: TopicsPage, failure: Failure | None = None) -> Case[TopicsPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_topics_listed, '__module__', "dex_connectors.google_pubsub.pubsubmock")

def list_topics_provider_rejected(value: TopicsPage, failure: Failure | None = None) -> Case[TopicsPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_topics_provider_rejected, '__module__', "dex_connectors.google_pubsub.pubsubmock")

def list_topics_invalid_response(value: TopicsPage, failure: Failure | None = None) -> Case[TopicsPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_topics_invalid_response, '__module__', "dex_connectors.google_pubsub.pubsubmock")

def list_topics_defect(value: TopicsPage, failure: Failure | None = None) -> Case[TopicsPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_topics_defect, '__module__', "dex_connectors.google_pubsub.pubsubmock")

def list_topics_retry(failure: Failure, after_seconds: float = 0) -> Case[TopicsPage]:
    return Case.retry(failure, after_seconds)

setattr(list_topics_retry, '__module__', "dex_connectors.google_pubsub.pubsubmock")

def list_topics_listed_default() -> Case[TopicsPage]:
    return Case.branch_result(LISTED, TopicsPage(topics=(Topic(name='projects/sample-project/topics/events'),), next_page_token=''))

setattr(list_topics_listed_default, '__module__', "dex_connectors.google_pubsub.pubsubmock")


class Mock:

    def __init__(self, connection_name: str = "google-pubsub") -> None:
        self.list_topics_script: Query[ListTopicsInput, TopicsPage] = Query(LIST_TOPICS_DEFINITION)
        self.list_topics_script.default(list_topics_listed_default())
        self._connection = new_connection_with_operations(
            list_topics=self.list_topics_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_pubsub.pubsubmock")

__all__ = [
    "Mock",
    "list_topics_listed",
    "list_topics_provider_rejected",
    "list_topics_invalid_response",
    "list_topics_defect",
    "list_topics_retry",
    "list_topics_listed_default",
]
