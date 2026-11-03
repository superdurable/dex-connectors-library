# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddReactionInput, AddedReaction, Channel, CreateMessageInput, GetChannelInput, ListChannelMessagesInput, Message, MessageAuthor, MessagePage
from .definitions import CONNECTOR_ID, ADDED, ADD_REACTION_DEFINITION, CREATED, CREATE_MESSAGE_DEFINITION, DEFECT, FOUND, GET_CHANNEL_DEFINITION, INVALID_RESPONSE, LISTED, LIST_CHANNEL_MESSAGES_DEFINITION, NOT_FOUND, PROVIDER_REJECTED

def get_channel_found(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_channel_found, '__module__', "dex_connectors.discord.discordmock")

def get_channel_not_found(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_channel_not_found, '__module__', "dex_connectors.discord.discordmock")

def get_channel_provider_rejected(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_channel_provider_rejected, '__module__', "dex_connectors.discord.discordmock")

def get_channel_invalid_response(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_channel_invalid_response, '__module__', "dex_connectors.discord.discordmock")

def get_channel_defect(value: Channel, failure: Failure | None = None) -> Case[Channel]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_channel_defect, '__module__', "dex_connectors.discord.discordmock")

def get_channel_retry(failure: Failure, after_seconds: float = 0) -> Case[Channel]:
    return Case.retry(failure, after_seconds)

setattr(get_channel_retry, '__module__', "dex_connectors.discord.discordmock")

def get_channel_announcement_channel() -> Case[Channel]:
    return Case.branch_result(FOUND, Channel(id='100000000000000001', type=0, guild_id='200000000000000001', name='announcements', topic='Release news'))

setattr(get_channel_announcement_channel, '__module__', "dex_connectors.discord.discordmock")

def get_channel_unknown_channel() -> Case[Channel]:
    return Case.branch_result(NOT_FOUND, Channel(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getChannel", "Discord found no such channel."))

setattr(get_channel_unknown_channel, '__module__', "dex_connectors.discord.discordmock")

def get_channel_inaccessible_channel() -> Case[Channel]:
    return Case.branch_result(PROVIDER_REJECTED, Channel(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getChannel", "Discord denied access to the channel."))

setattr(get_channel_inaccessible_channel, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_listed(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_channel_messages_listed, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_not_found(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_channel_messages_not_found, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_provider_rejected(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_channel_messages_provider_rejected, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_invalid_response(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_channel_messages_invalid_response, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_defect(value: MessagePage, failure: Failure | None = None) -> Case[MessagePage]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_channel_messages_defect, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_retry(failure: Failure, after_seconds: float = 0) -> Case[MessagePage]:
    return Case.retry(failure, after_seconds)

setattr(list_channel_messages_retry, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_announcement_read_back() -> Case[MessagePage]:
    return Case.branch_result(LISTED, MessagePage(messages=(Message(id='300000000000000002', channel_id='100000000000000001', author=MessageAuthor(id='400000000000000002', username='teammate', is_bot=False), content='Thanks for the announcement!', timestamp=datetime.fromisoformat("2026-10-26T12:01:00Z".replace('Z', '+00:00')), type=0), Message(id='300000000000000001', channel_id='100000000000000001', author=MessageAuthor(id='400000000000000001', username='releasebot', is_bot=True), content='Release today @everyone', timestamp=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00')), type=0)), oldest_message_id='300000000000000001', is_full=False))

setattr(list_channel_messages_announcement_read_back, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_unknown_channel() -> Case[MessagePage]:
    return Case.branch_result(NOT_FOUND, MessagePage(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listChannelMessages", "Discord found no such channel."))

setattr(list_channel_messages_unknown_channel, '__module__', "dex_connectors.discord.discordmock")

def list_channel_messages_missing_read_permission() -> Case[MessagePage]:
    return Case.branch_result(PROVIDER_REJECTED, MessagePage(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listChannelMessages", "Discord denied message history access."))

setattr(list_channel_messages_missing_read_permission, '__module__', "dex_connectors.discord.discordmock")

def create_message_created(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_message_created, '__module__', "dex_connectors.discord.discordmock")

def create_message_not_found(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(create_message_not_found, '__module__', "dex_connectors.discord.discordmock")

def create_message_provider_rejected(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_message_provider_rejected, '__module__', "dex_connectors.discord.discordmock")

def create_message_invalid_response(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_message_invalid_response, '__module__', "dex_connectors.discord.discordmock")

def create_message_defect(value: Message, failure: Failure | None = None) -> Case[Message]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_message_defect, '__module__', "dex_connectors.discord.discordmock")

def create_message_retry(failure: Failure, after_seconds: float = 0) -> Case[Message]:
    return Case.retry(failure, after_seconds)

setattr(create_message_retry, '__module__', "dex_connectors.discord.discordmock")

def create_message_release_announcement() -> Case[Message]:
    return Case.branch_result(CREATED, Message(id='300000000000000001', channel_id='100000000000000001', author=MessageAuthor(id='400000000000000001', username='releasebot', is_bot=True), content='Release today @everyone', timestamp=datetime.fromisoformat("2026-10-26T12:00:00Z".replace('Z', '+00:00')), type=0))

setattr(create_message_release_announcement, '__module__', "dex_connectors.discord.discordmock")

def create_message_unknown_channel() -> Case[Message]:
    return Case.branch_result(NOT_FOUND, Message(author=MessageAuthor()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createMessage", "Discord found no such channel."))

setattr(create_message_unknown_channel, '__module__', "dex_connectors.discord.discordmock")

def create_message_missing_send_permission() -> Case[Message]:
    return Case.branch_result(PROVIDER_REJECTED, Message(author=MessageAuthor()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createMessage", "Discord denied message creation."))

setattr(create_message_missing_send_permission, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_added(value: AddedReaction, failure: Failure | None = None) -> Case[AddedReaction]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_reaction_added, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_not_found(value: AddedReaction, failure: Failure | None = None) -> Case[AddedReaction]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_reaction_not_found, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_provider_rejected(value: AddedReaction, failure: Failure | None = None) -> Case[AddedReaction]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_reaction_provider_rejected, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_defect(value: AddedReaction, failure: Failure | None = None) -> Case[AddedReaction]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_reaction_defect, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_retry(failure: Failure, after_seconds: float = 0) -> Case[AddedReaction]:
    return Case.retry(failure, after_seconds)

setattr(add_reaction_retry, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_announcement_reaction() -> Case[AddedReaction]:
    return Case.branch_result(ADDED, AddedReaction(channel_id='100000000000000001', message_id='300000000000000001', emoji='📣'))

setattr(add_reaction_announcement_reaction, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_unknown_message() -> Case[AddedReaction]:
    return Case.branch_result(NOT_FOUND, AddedReaction(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addReaction", "Discord found no such message."))

setattr(add_reaction_unknown_message, '__module__', "dex_connectors.discord.discordmock")

def add_reaction_missing_reaction_permission() -> Case[AddedReaction]:
    return Case.branch_result(PROVIDER_REJECTED, AddedReaction(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addReaction", "Discord denied the reaction."))

setattr(add_reaction_missing_reaction_permission, '__module__', "dex_connectors.discord.discordmock")


class Mock:

    def __init__(self, connection_name: str = "discord") -> None:
        self.get_channel_script: Query[GetChannelInput, Channel] = Query(GET_CHANNEL_DEFINITION)
        self.get_channel_script.default(get_channel_announcement_channel())
        self.list_channel_messages_script: Query[ListChannelMessagesInput, MessagePage] = Query(LIST_CHANNEL_MESSAGES_DEFINITION)
        self.list_channel_messages_script.default(list_channel_messages_announcement_read_back())
        self.create_message_script: Mutation[CreateMessageInput, Message] = Mutation(CREATE_MESSAGE_DEFINITION)
        self.create_message_script.default(create_message_release_announcement())
        self.add_reaction_script: Mutation[AddReactionInput, AddedReaction] = Mutation(ADD_REACTION_DEFINITION)
        self.add_reaction_script.default(add_reaction_announcement_reaction())
        self._connection = new_connection_with_operations(
            get_channel=self.get_channel_script,
            list_channel_messages=self.list_channel_messages_script,
            create_message=self.create_message_script,
            add_reaction=self.add_reaction_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.discord.discordmock")

__all__ = [
    "Mock",
    "get_channel_found",
    "get_channel_not_found",
    "get_channel_provider_rejected",
    "get_channel_invalid_response",
    "get_channel_defect",
    "get_channel_retry",
    "get_channel_announcement_channel",
    "get_channel_unknown_channel",
    "get_channel_inaccessible_channel",
    "list_channel_messages_listed",
    "list_channel_messages_not_found",
    "list_channel_messages_provider_rejected",
    "list_channel_messages_invalid_response",
    "list_channel_messages_defect",
    "list_channel_messages_retry",
    "list_channel_messages_announcement_read_back",
    "list_channel_messages_unknown_channel",
    "list_channel_messages_missing_read_permission",
    "create_message_created",
    "create_message_not_found",
    "create_message_provider_rejected",
    "create_message_invalid_response",
    "create_message_defect",
    "create_message_retry",
    "create_message_release_announcement",
    "create_message_unknown_channel",
    "create_message_missing_send_permission",
    "add_reaction_added",
    "add_reaction_not_found",
    "add_reaction_provider_rejected",
    "add_reaction_defect",
    "add_reaction_retry",
    "add_reaction_announcement_reaction",
    "add_reaction_unknown_message",
    "add_reaction_missing_reaction_permission",
]
