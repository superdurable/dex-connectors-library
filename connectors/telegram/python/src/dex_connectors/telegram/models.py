# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Telegram Bot API models generated from the Go connector contract."""

from ._generated.models import Chat as Chat
from ._generated.models import GetChatInput as GetChatInput
from ._generated.models import SendMessageInput as SendMessageInput
from ._generated.models import SentMessage as SentMessage

Chat.__doc__ = "Basic non-secret metadata for one accessible Telegram chat."
GetChatInput.__doc__ = "A numeric chat ID or public @username."
SendMessageInput.__doc__ = "One chat destination and plain text message."
SentMessage.__doc__ = "The numeric chat and message IDs with Telegram's Unix creation time."
