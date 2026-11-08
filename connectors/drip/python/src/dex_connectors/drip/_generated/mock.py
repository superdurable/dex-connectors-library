# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AccountMetadata, AccountsList, GetSubscriberInput, ListAccountsInput, SubscriberMetadata
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_SUBSCRIBER_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ACCOUNTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_accounts_listed(value: AccountsList, failure: Failure | None = None) -> Case[AccountsList]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_accounts_listed, '__module__', "dex_connectors.drip.dripmock")

def list_accounts_provider_rejected(value: AccountsList, failure: Failure | None = None) -> Case[AccountsList]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_accounts_provider_rejected, '__module__', "dex_connectors.drip.dripmock")

def list_accounts_invalid_response(value: AccountsList, failure: Failure | None = None) -> Case[AccountsList]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_accounts_invalid_response, '__module__', "dex_connectors.drip.dripmock")

def list_accounts_defect(value: AccountsList, failure: Failure | None = None) -> Case[AccountsList]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_accounts_defect, '__module__', "dex_connectors.drip.dripmock")

def list_accounts_retry(failure: Failure, after_seconds: float = 0) -> Case[AccountsList]:
    return Case.retry(failure, after_seconds)

setattr(list_accounts_retry, '__module__', "dex_connectors.drip.dripmock")

def list_accounts_listed_default() -> Case[AccountsList]:
    return Case.branch_result(LISTED, AccountsList(accounts=(AccountMetadata(id='9999999', name='Sample Account'),)))

setattr(list_accounts_listed_default, '__module__', "dex_connectors.drip.dripmock")

def get_subscriber_found(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_subscriber_found, '__module__', "dex_connectors.drip.dripmock")

def get_subscriber_not_found(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_subscriber_not_found, '__module__', "dex_connectors.drip.dripmock")

def get_subscriber_provider_rejected(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_subscriber_provider_rejected, '__module__', "dex_connectors.drip.dripmock")

def get_subscriber_invalid_response(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_subscriber_invalid_response, '__module__', "dex_connectors.drip.dripmock")

def get_subscriber_defect(value: SubscriberMetadata, failure: Failure | None = None) -> Case[SubscriberMetadata]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_subscriber_defect, '__module__', "dex_connectors.drip.dripmock")

def get_subscriber_retry(failure: Failure, after_seconds: float = 0) -> Case[SubscriberMetadata]:
    return Case.retry(failure, after_seconds)

setattr(get_subscriber_retry, '__module__', "dex_connectors.drip.dripmock")

def get_subscriber_found_default() -> Case[SubscriberMetadata]:
    return Case.branch_result(FOUND, SubscriberMetadata(id='sample-subscriber-id', email='ada@example.com', first_name='Ada', last_name='Lovelace', status='active'))

setattr(get_subscriber_found_default, '__module__', "dex_connectors.drip.dripmock")


class Mock:

    def __init__(self, connection_name: str = "drip") -> None:
        self.list_accounts_script: Query[ListAccountsInput, AccountsList] = Query(LIST_ACCOUNTS_DEFINITION)
        self.list_accounts_script.default(list_accounts_listed_default())
        self.get_subscriber_script: Query[GetSubscriberInput, SubscriberMetadata] = Query(GET_SUBSCRIBER_DEFINITION)
        self.get_subscriber_script.default(get_subscriber_found_default())
        self._connection = new_connection_with_operations(
            list_accounts=self.list_accounts_script,
            get_subscriber=self.get_subscriber_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.drip.dripmock")

__all__ = [
    "Mock",
    "list_accounts_listed",
    "list_accounts_provider_rejected",
    "list_accounts_invalid_response",
    "list_accounts_defect",
    "list_accounts_retry",
    "list_accounts_listed_default",
    "get_subscriber_found",
    "get_subscriber_not_found",
    "get_subscriber_provider_rejected",
    "get_subscriber_invalid_response",
    "get_subscriber_defect",
    "get_subscriber_retry",
    "get_subscriber_found_default",
]
