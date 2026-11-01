# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Postmark outbound message searches and details."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListOutboundMessagesInput:
    """One page of outbound messages; count zero means 50, and count plus offset is at most 10,000."""

    count: int = 0
    offset: int = 0


@dataclass(frozen=True, slots=True)
class GetMessageDetailsInput:
    """Postmark outbound message ID."""

    message_id: str = field(default="", metadata={"dex_json_name": "messageId"})


@dataclass(frozen=True, slots=True)
class MessageDetails:
    """Outbound message summary with Postmark's delivery status text."""

    message_id: str = field(default="", metadata={"dex_json_name": "MessageID"})
    subject: str = field(default="", metadata={"dex_json_name": "Subject"})
    status: str = field(default="", metadata={"dex_json_name": "Status"})


@dataclass(frozen=True, slots=True)
class MessagePage:
    """One page of outbound messages and Postmark's total match count."""

    total_count: int = field(default=0, metadata={"dex_json_name": "TotalCount"})
    messages: tuple[MessageDetails, ...] = field(default=(), metadata={"dex_json_name": "Messages"})
