# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Manifest-backed Postmark scripts for consuming Flow tests."""

from dex_connectors.sdk import ConnectionRef, Failure
from dex_connectors.sdk.connectormock import Case, Query

from ..client import (
    CONNECTOR_ID,
    FOUND,
    GET_MESSAGE_DETAILS_DEFINITION,
    LIST_OUTBOUND_MESSAGES_DEFINITION,
    LISTED,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from ..connection import Connection, new_connection_with_operations
from ..models import GetMessageDetailsInput, ListOutboundMessagesInput, MessageDetails, MessagePage

_DEFAULT_MESSAGE = MessageDetails("0ac29aee-e1cd-480d-b08d-4f48548ff48d", "Welcome", "Sent")


def list_outbound_messages_listed(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    """Script the successful listed route."""
    return Case.branch_result(LISTED, value, failure)


def list_outbound_messages_provider_rejected(
    value: MessagePage, failure: Failure | None = None
) -> Case[MessagePage]:
    """Script a definite provider rejection of the search."""
    return Case.branch_result(PROVIDER_REJECTED, value, failure)


def list_outbound_messages_default() -> Case[MessagePage]:
    """Return the manifest's listOutboundMessagesDefault answer."""
    return list_outbound_messages_listed(MessagePage(1, (_DEFAULT_MESSAGE,)))


def get_message_details_found(value: MessageDetails, failure: Failure | None = None) -> Case[MessageDetails]:
    """Script the successful found route."""
    return Case.branch_result(FOUND, value, failure)


def get_message_details_not_found(
    value: MessageDetails, failure: Failure | None = None
) -> Case[MessageDetails]:
    """Script a missing outbound message."""
    return Case.branch_result(NOT_FOUND, value, failure)


def get_message_details_default() -> Case[MessageDetails]:
    """Return the manifest's getMessageDetailsDefault answer."""
    return get_message_details_found(_DEFAULT_MESSAGE)


class Mock:
    """Script Postmark operations, defaulting to their manifest answers."""

    def __init__(self, connection_name: str = "postmark") -> None:
        self.list_outbound_messages_script: Query[ListOutboundMessagesInput, MessagePage] = Query(
            LIST_OUTBOUND_MESSAGES_DEFINITION
        )
        self.list_outbound_messages_script.default(list_outbound_messages_default())
        self.get_message_details_script: Query[GetMessageDetailsInput, MessageDetails] = Query(
            GET_MESSAGE_DETAILS_DEFINITION
        )
        self.get_message_details_script.default(get_message_details_default())
        self._connection = new_connection_with_operations(
            self.list_outbound_messages_script,
            self.get_message_details_script,
            ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        """Return this mock's named connection for Step registration."""
        return self._connection


__all__ = [
    "Mock",
    "list_outbound_messages_listed",
    "list_outbound_messages_provider_rejected",
    "list_outbound_messages_default",
    "get_message_details_found",
    "get_message_details_not_found",
    "get_message_details_default",
]
