# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# SendMessageInput sends plain text to one bot-accessible chat.
@dataclass(frozen=True, slots=True)
class SendMessageInput:
    # ChatID is a decimal chat ID or public @username.
    chat_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'chatId'})
    # Text is 1 through 4096 UTF-8 characters; Telegram may further reject invalid entities.
    text: str = ''


setattr(SendMessageInput, '__module__', 'dex_connectors.telegram.models')


# SentMessage identifies a Telegram message created by sendMessage.
@dataclass(frozen=True, slots=True)
class SentMessage:
    # ChatID is the numeric destination chat ID returned by Telegram.
    chat_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'chatId'})
    # MessageID identifies the message within its chat.
    message_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'messageId'})
    # Date is the provider's Unix creation time in seconds.
    date: int = 0


setattr(SentMessage, '__module__', 'dex_connectors.telegram.models')


# GetChatInput selects a numeric chat ID or public @username.
@dataclass(frozen=True, slots=True)
class GetChatInput:
    # ChatID is a decimal chat ID or an @username visible to the bot.
    chat_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'chatId'})


setattr(GetChatInput, '__module__', 'dex_connectors.telegram.models')


# Chat contains basic non-secret metadata returned by Telegram getChat.
@dataclass(frozen=True, slots=True)
class Chat:
    # ID is Telegram's stable numeric chat identifier.
    id: int = 0
    # Type is private, group, supergroup, or channel.
    type: str = ''
    # Title is the current display title for a group or channel.
    title: str = ''
    # Username is the optional public @username without its prefix.
    username: str = ''


setattr(Chat, '__module__', 'dex_connectors.telegram.models')
