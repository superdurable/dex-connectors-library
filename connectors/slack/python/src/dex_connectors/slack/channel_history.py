# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The listChannelMessages Query: one bounded, newest-first page of a channel's top-level messages."""

from __future__ import annotations

import re
from typing import TYPE_CHECKING, Any, Final

from dex_connectors.sdk import Call, FailureKind, QueryAttempt, QueryDefinition

from ._generated.definitions import DEFECT, INVALID_RESPONSE, LIST_CHANNEL_MESSAGES_DEFINITION, READ
from .models import ChannelMessage, ListChannelMessagesInput, ListChannelMessagesOutput

if TYPE_CHECKING:
    from .client import Client

# Slack caps conversations.history at 15 messages for non-Marketplace apps installed since May 2025.
_MAXIMUM_CHANNEL_HISTORY_PAGE_SIZE: Final = 15
_SLACK_TIMESTAMP: Final = re.compile(r"[0-9]+(\.[0-9]+)?", re.ASCII)
_INT64_LIMIT: Final = 2**63


def _is_valid_input(input: ListChannelMessagesInput) -> bool:
    if not input.channel_id.strip() or not 0 <= input.page_size <= _MAXIMUM_CHANNEL_HISTORY_PAGE_SIZE:
        return False
    return all(
        not timestamp or _SLACK_TIMESTAMP.fullmatch(timestamp) is not None
        for timestamp in (input.oldest_timestamp, input.latest_timestamp)
    )


def _history_field(value: dict[str, Any], name: str) -> str:
    item = value.get(name)
    if item is None:
        return ""
    if not isinstance(item, str):
        raise ValueError("Slack channel message has an invalid field")
    return item


def _decode_channel_history(channel_id: str, value: Any) -> list[ChannelMessage]:
    """Decode conversations.history messages with Go json.Unmarshal type checks; null is the zero value."""
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError("Slack channel history has an invalid shape")
    messages = []
    for raw in value:
        item = {} if raw is None else raw
        if not isinstance(item, dict):
            raise ValueError("Slack channel message has an invalid shape")
        reply_count = item.get("reply_count")
        if reply_count is None:
            reply_count = 0
        if (
            isinstance(reply_count, bool)
            or not isinstance(reply_count, int)
            or abs(reply_count) >= _INT64_LIMIT
        ):
            raise ValueError("Slack channel message has an invalid reply count")
        messages.append(
            ChannelMessage(
                channel_id,
                _history_field(item, "ts"),
                _history_field(item, "thread_ts"),
                _history_field(item, "user"),
                _history_field(item, "bot_id"),
                _history_field(item, "text"),
                reply_count,
            )
        )
    return messages


class ListChannelMessagesOperation:
    """Read one bounded, newest-first page of a channel's top-level messages with the bot token."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation definition."""
        return LIST_CHANNEL_MESSAGES_DEFINITION

    async def invoke(
        self, call: Call, input: ListChannelMessagesInput
    ) -> QueryAttempt[ListChannelMessagesOutput]:
        """Validate, read conversations.history, and classify one provider attempt."""
        from .client import _classify_query_response, _failure, _query_branch

        operation = "listChannelMessages"
        if not _is_valid_input(input):
            return _query_branch(
                operation,
                DEFECT,
                FailureKind.VALIDATION,
                "channel, valid Slack message timestamps, and page size 1 through 15 (blank or 0 reads 15) "
                "are required",
            )
        credentials, _ = await self.client._resolve(call, "bot_token")
        if credentials is None:
            return _query_branch(
                operation, DEFECT, FailureKind.AUTHENTICATION, "Slack connection credentials are unavailable"
            )
        query = {
            "channel": input.channel_id,
            "limit": str(input.page_size or _MAXIMUM_CHANNEL_HISTORY_PAGE_SIZE),
        }
        if input.oldest_timestamp:
            query["oldest"] = input.oldest_timestamp
        if input.latest_timestamp:
            query["latest"] = input.latest_timestamp
        if input.is_inclusive:
            query["inclusive"] = "true"
        if input.cursor:
            query["cursor"] = input.cursor
        response = await self.client._get(call, credentials, query, "bot_token", "conversations.history")
        classified = _classify_query_response(
            self.client,
            call,
            operation,
            response,
            ListChannelMessagesOutput(),
            "Slack channel history is temporarily unavailable",
            "Slack rate limited the channel history query",
        )
        if classified is not None:
            return classified
        try:
            messages = _decode_channel_history(input.channel_id, response.decoded.get("messages"))
        except ValueError:
            return QueryAttempt.branch_result(
                INVALID_RESPONSE,
                ListChannelMessagesOutput(),
                self.client._receipt(call, response),
                _failure(operation, FailureKind.PROTOCOL, "Slack returned an invalid channel history"),
            )
        if any(not message.timestamp for message in messages):
            return QueryAttempt.branch_result(
                INVALID_RESPONSE,
                ListChannelMessagesOutput(),
                self.client._receipt(call, response),
                _failure(
                    operation, FailureKind.PROTOCOL, "Slack returned a channel message without a timestamp"
                ),
            )
        output = ListChannelMessagesOutput(
            tuple(messages), (response.decoded.get("response_metadata") or {}).get("next_cursor") or ""
        )
        return QueryAttempt.branch_result(READ, output, self.client._receipt(call, response))
