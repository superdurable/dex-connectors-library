# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed input and output for the Gmail read-and-reply example Flow."""

from dataclasses import dataclass, field

CONNECTION_NAME = "gmail-inbox"


@dataclass(frozen=True, slots=True)
class ThreadReplyInput:
    """Identify a received Gmail message and the plain-text reply to send."""

    # Provider message identifier read before replying.
    message_id: str = field(metadata={"dex_json_name": "messageId"})
    # Reply body sent to the message's Reply-To or From mailbox.
    reply_text: str = field(metadata={"dex_json_name": "replyText"})


@dataclass(frozen=True, slots=True)
class ThreadReplyOutput:
    """Return the read source and accepted reply IDs after the Flow completes."""

    # Gmail thread shared by source and reply.
    thread_id: str = field(metadata={"dex_json_name": "threadId"})
    # Gmail identifier of the accepted reply; it can be empty after an unusable accepted response.
    reply_message_id: str = field(metadata={"dex_json_name": "replyMessageId"})
