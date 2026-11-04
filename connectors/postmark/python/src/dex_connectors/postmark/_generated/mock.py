# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetMessageDetailsInput, ListOutboundMessagesInput, MessageDetails, MessagePage
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_MESSAGE_DETAILS_DEFINITION, INVALID_RESPONSE, LISTED, LIST_OUTBOUND_MESSAGES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def list_outbound_messages_listed(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_outbound_messages_listed, '__module__', "dex_connectors.postmark.postmarkmock")

def list_outbound_messages_provider_rejected(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_outbound_messages_provider_rejected, '__module__', "dex_connectors.postmark.postmarkmock")

def list_outbound_messages_invalid_response(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_outbound_messages_invalid_response, '__module__', "dex_connectors.postmark.postmarkmock")

def list_outbound_messages_defect(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_outbound_messages_defect, '__module__', "dex_connectors.postmark.postmarkmock")

def list_outbound_messages_retry(failure: Failure, after_seconds: float = 0) -> Case[MessagePage]:
    return Case.retry(failure, after_seconds)

setattr(list_outbound_messages_retry, '__module__', "dex_connectors.postmark.postmarkmock")

def list_outbound_messages_default() -> Case[MessagePage]:
    return Case.branch_result(LISTED, MessagePage(total_count=1, messages=(MessageDetails(message_id='0ac29aee-e1cd-480d-b08d-4f48548ff48d', subject='Welcome', status='Sent'),)))

setattr(list_outbound_messages_default, '__module__', "dex_connectors.postmark.postmarkmock")

def get_message_details_found(value: MessageDetails, failure: Failure | None = None) -> Case[MessageDetails]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_message_details_found, '__module__', "dex_connectors.postmark.postmarkmock")

def get_message_details_not_found(value: MessageDetails, failure: Failure | None = None) -> Case[MessageDetails]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_message_details_not_found, '__module__', "dex_connectors.postmark.postmarkmock")

def get_message_details_provider_rejected(value: MessageDetails, failure: Failure | None = None) -> Case[MessageDetails]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_message_details_provider_rejected, '__module__', "dex_connectors.postmark.postmarkmock")

def get_message_details_invalid_response(value: MessageDetails, failure: Failure | None = None) -> Case[MessageDetails]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_message_details_invalid_response, '__module__', "dex_connectors.postmark.postmarkmock")

def get_message_details_defect(value: MessageDetails, failure: Failure | None = None) -> Case[MessageDetails]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_message_details_defect, '__module__', "dex_connectors.postmark.postmarkmock")

def get_message_details_retry(failure: Failure, after_seconds: float = 0) -> Case[MessageDetails]:
    return Case.retry(failure, after_seconds)

setattr(get_message_details_retry, '__module__', "dex_connectors.postmark.postmarkmock")

def get_message_details_default() -> Case[MessageDetails]:
    return Case.branch_result(FOUND, MessageDetails(message_id='0ac29aee-e1cd-480d-b08d-4f48548ff48d', subject='Welcome', status='Sent'))

setattr(get_message_details_default, '__module__', "dex_connectors.postmark.postmarkmock")


class Mock:

    def __init__(self, connection_name: str = "postmark") -> None:
        self.list_outbound_messages_script: Query[ListOutboundMessagesInput, MessagePage] = Query(LIST_OUTBOUND_MESSAGES_DEFINITION)
        self.list_outbound_messages_script.default(list_outbound_messages_default())
        self.get_message_details_script: Query[GetMessageDetailsInput, MessageDetails] = Query(GET_MESSAGE_DETAILS_DEFINITION)
        self.get_message_details_script.default(get_message_details_default())
        self._connection = new_connection_with_operations(
            list_outbound_messages=self.list_outbound_messages_script,
            get_message_details=self.get_message_details_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.postmark.postmarkmock")

__all__ = [
    "Mock",
    "list_outbound_messages_listed",
    "list_outbound_messages_provider_rejected",
    "list_outbound_messages_invalid_response",
    "list_outbound_messages_defect",
    "list_outbound_messages_retry",
    "list_outbound_messages_default",
    "get_message_details_found",
    "get_message_details_not_found",
    "get_message_details_provider_rejected",
    "get_message_details_invalid_response",
    "get_message_details_defect",
    "get_message_details_retry",
    "get_message_details_default",
]
