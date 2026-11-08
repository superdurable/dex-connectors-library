# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetSubscriberInput, ListSubscribersInput, SubscriberMetadata, SubscribersPage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SUBSCRIBER_DEFINITION, INVALID_RESPONSE, LISTED, LIST_SUBSCRIBERS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_subscribers_listed(value: SubscribersPage, failure: Failure | None = None) -> Case[SubscribersPage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_subscribers_listed, '__module__', "dex_connectors.kit.kitmock")

def list_subscribers_provider_rejected(value: SubscribersPage, failure: Failure | None = None) -> Case[SubscribersPage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_subscribers_provider_rejected, '__module__', "dex_connectors.kit.kitmock")

def list_subscribers_invalid_response(value: SubscribersPage, failure: Failure | None = None) -> Case[SubscribersPage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_subscribers_invalid_response, '__module__', "dex_connectors.kit.kitmock")

def list_subscribers_defect(value: SubscribersPage, failure: Failure | None = None) -> Case[SubscribersPage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_subscribers_defect, '__module__', "dex_connectors.kit.kitmock")

def list_subscribers_retry(failure: Failure, after_seconds: float = 0) -> Case[SubscribersPage]:
    return Case.retry(failure, after_seconds)

setattr(list_subscribers_retry, '__module__', "dex_connectors.kit.kitmock")

def list_subscribers_listed_default() -> Case[SubscribersPage]:
    return Case.branch_result(LISTED, SubscribersPage(subscribers=(SubscriberMetadata(id=204, email_address='alice@example.com', first_name='Alice', state='active'),), has_next_page=False, next_cursor='', per_page=20))

setattr(list_subscribers_listed_default, '__module__', "dex_connectors.kit.kitmock")

def get_subscriber_found(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_subscriber_found, '__module__', "dex_connectors.kit.kitmock")

def get_subscriber_not_found(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_subscriber_not_found, '__module__', "dex_connectors.kit.kitmock")

def get_subscriber_provider_rejected(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_subscriber_provider_rejected, '__module__', "dex_connectors.kit.kitmock")

def get_subscriber_invalid_response(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_subscriber_invalid_response, '__module__', "dex_connectors.kit.kitmock")

def get_subscriber_defect(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_subscriber_defect, '__module__', "dex_connectors.kit.kitmock")

def get_subscriber_retry(failure: Failure, after_seconds: float = 0) -> Case[SubscriberMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_subscriber_retry, '__module__', "dex_connectors.kit.kitmock")

def get_subscriber_found_default() -> Case[SubscriberMetadata]:
    return Case.branch_result(FOUND, SubscriberMetadata(id=204, email_address='alice@example.com', first_name='Alice', state='active'))

setattr(get_subscriber_found_default, '__module__', "dex_connectors.kit.kitmock")


class Mock:

    def __init__(self, connection_name: str = "kit") -> None:
        self.list_subscribers_script: Query[ListSubscribersInput, SubscribersPage] = Query(LIST_SUBSCRIBERS_DEFINITION)
        self.list_subscribers_script.default(list_subscribers_listed_default())
        self.get_subscriber_script: Query[GetSubscriberInput, SubscriberMetadata] = Query(GET_SUBSCRIBER_DEFINITION)
        self.get_subscriber_script.default(get_subscriber_found_default())
        self._connection = new_connection_with_operations(
            list_subscribers=self.list_subscribers_script,
            get_subscriber=self.get_subscriber_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.kit.kitmock")

__all__ = [
    "Mock",
    "list_subscribers_listed",
    "list_subscribers_provider_rejected",
    "list_subscribers_invalid_response",
    "list_subscribers_defect",
    "list_subscribers_retry",
    "list_subscribers_listed_default",
    "get_subscriber_found",
    "get_subscriber_not_found",
    "get_subscriber_provider_rejected",
    "get_subscriber_invalid_response",
    "get_subscriber_defect",
    "get_subscriber_retry",
    "get_subscriber_found_default",
]
