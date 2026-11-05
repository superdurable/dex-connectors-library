# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_MESSAGE_DEFINITION, MOVE_MESSAGE_DEFINITION, REPLY_TO_MESSAGE_DEFINITION, SEARCH_MESSAGES_DEFINITION, SEND_MESSAGE_DEFINITION, SET_FLAGS_DEFINITION
from ..models import GetMessageInput, Message, MessageFlags, MoveMessageInput, MovedMessage, ReplyToMessageInput, SearchMessagesInput, SearchMessagesOutput, SendMessageInput, SentMessage, SetFlagsInput

class Connection:

    __slots__ = ('_search_messages', '_get_message', '_send_message', '_reply_to_message', '_move_message', '_set_flags', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_messages: Query[SearchMessagesInput, SearchMessagesOutput],
        get_message: Query[GetMessageInput, Message],
        send_message: Mutation[SendMessageInput, SentMessage],
        reply_to_message: Mutation[ReplyToMessageInput, SentMessage],
        move_message: Mutation[MoveMessageInput, MovedMessage],
        set_flags: Mutation[SetFlagsInput, MessageFlags],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_messages is None or get_message is None or send_message is None or reply_to_message is None or move_message is None or set_flags is None:
            raise ValueError("Email (IMAP and SMTP) connection provider and operations are required")
        if search_messages.definition().operation != SEARCH_MESSAGES_DEFINITION.operation:
            raise ValueError("Email (IMAP and SMTP) connection operation identity differs")
        if get_message.definition().operation != GET_MESSAGE_DEFINITION.operation:
            raise ValueError("Email (IMAP and SMTP) connection operation identity differs")
        if send_message.definition().operation != SEND_MESSAGE_DEFINITION.operation:
            raise ValueError("Email (IMAP and SMTP) connection operation identity differs")
        if reply_to_message.definition().operation != REPLY_TO_MESSAGE_DEFINITION.operation:
            raise ValueError("Email (IMAP and SMTP) connection operation identity differs")
        if move_message.definition().operation != MOVE_MESSAGE_DEFINITION.operation:
            raise ValueError("Email (IMAP and SMTP) connection operation identity differs")
        if set_flags.definition().operation != SET_FLAGS_DEFINITION.operation:
            raise ValueError("Email (IMAP and SMTP) connection operation identity differs")
        self.reference = reference
        self._search_messages = search_messages
        self._get_message = get_message
        self._send_message = send_message
        self._reply_to_message = reply_to_message
        self._move_message = move_message
        self._set_flags = set_flags
        self._client = client

    def search_messages(self) -> Query[SearchMessagesInput, SearchMessagesOutput]:
        return self._search_messages

    def get_message(self) -> Query[GetMessageInput, Message]:
        return self._get_message

    def send_message(self) -> Mutation[SendMessageInput, SentMessage]:
        return self._send_message

    def reply_to_message(self) -> Mutation[ReplyToMessageInput, SentMessage]:
        return self._reply_to_message

    def move_message(self) -> Mutation[MoveMessageInput, MovedMessage]:
        return self._move_message

    def set_flags(self) -> Mutation[SetFlagsInput, MessageFlags]:
        return self._set_flags

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "superdurable_email.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Email (IMAP and SMTP) connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.superdurable_email.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Email (IMAP and SMTP) client is required")
    return Connection(reference, client.search_messages(), client.get_message(), client.send_message(), client.reply_to_message(), client.move_message(), client.set_flags(), client)

setattr(new_connection, '__module__', 'dex_connectors.superdurable_email.connection')


def new_connection_with_operations(
    search_messages: Query[SearchMessagesInput, SearchMessagesOutput],
    get_message: Query[GetMessageInput, Message],
    send_message: Mutation[SendMessageInput, SentMessage],
    reply_to_message: Mutation[ReplyToMessageInput, SentMessage],
    move_message: Mutation[MoveMessageInput, MovedMessage],
    set_flags: Mutation[SetFlagsInput, MessageFlags],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_messages, get_message, send_message, reply_to_message, move_message, set_flags)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.superdurable_email.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        username = material.get("username")
        if not isinstance(username, str) or not username:
            raise ValueError("Email (IMAP and SMTP) credential username is required")
        password = material.get("password")
        if not isinstance(password, str) or not password:
            raise ValueError("Email (IMAP and SMTP) credential password is required")
        smtp_username = material.get("smtp_username")
        if smtp_username is None:
            smtp_username = ''
        if not isinstance(smtp_username, str):
            raise ValueError("Email (IMAP and SMTP) credential smtp_username has an invalid type")
        smtp_password = material.get("smtp_password")
        if smtp_password is None:
            smtp_password = ''
        if not isinstance(smtp_password, str):
            raise ValueError("Email (IMAP and SMTP) credential smtp_password has an invalid type")
        return Credentials(username, SecretString(password), smtp_username, SecretString(smtp_password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Email (IMAP and SMTP) connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Email (IMAP and SMTP) project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Email (IMAP and SMTP) project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.superdurable_email.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"imapHost", "imapPort", "imapSecurity", "smtpHost", "smtpPort", "smtpSecurity", "fromAddress", "fromName"}:
        raise ValueError("Email (IMAP and SMTP) configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "imapHost" in value:
        imap_host = value["imapHost"]
        if not (isinstance(imap_host, str) and bool(imap_host)):
            raise ValueError("Email (IMAP and SMTP) configuration imapHost has an invalid type")
        config_arguments['imap_host'] = imap_host
    else:
        raise ValueError("Email (IMAP and SMTP) configuration imapHost is required")
    if "imapPort" in value and value["imapPort"] != '':
        imap_port = value["imapPort"]
        if not (type(imap_port) is int):
            raise ValueError("Email (IMAP and SMTP) configuration imapPort has an invalid type")
        config_arguments['imap_port'] = imap_port
    if "imapSecurity" in value and value["imapSecurity"] != '':
        imap_security = value["imapSecurity"]
        if not (isinstance(imap_security, str) and imap_security in ('implicitTLS', 'startTLS')):
            raise ValueError("Email (IMAP and SMTP) configuration imapSecurity has an invalid type")
        config_arguments['imap_security'] = imap_security
    if "smtpHost" in value:
        smtp_host = value["smtpHost"]
        if not (isinstance(smtp_host, str) and bool(smtp_host)):
            raise ValueError("Email (IMAP and SMTP) configuration smtpHost has an invalid type")
        config_arguments['smtp_host'] = smtp_host
    else:
        raise ValueError("Email (IMAP and SMTP) configuration smtpHost is required")
    if "smtpPort" in value and value["smtpPort"] != '':
        smtp_port = value["smtpPort"]
        if not (type(smtp_port) is int):
            raise ValueError("Email (IMAP and SMTP) configuration smtpPort has an invalid type")
        config_arguments['smtp_port'] = smtp_port
    if "smtpSecurity" in value and value["smtpSecurity"] != '':
        smtp_security = value["smtpSecurity"]
        if not (isinstance(smtp_security, str) and smtp_security in ('implicitTLS', 'startTLS')):
            raise ValueError("Email (IMAP and SMTP) configuration smtpSecurity has an invalid type")
        config_arguments['smtp_security'] = smtp_security
    if "fromAddress" in value and value["fromAddress"] != '':
        from_address = value["fromAddress"]
        if not (isinstance(from_address, str)):
            raise ValueError("Email (IMAP and SMTP) configuration fromAddress has an invalid type")
        config_arguments['from_address'] = from_address
    else:
        config_arguments['from_address'] = ''
    if "fromName" in value and value["fromName"] != '':
        from_name = value["fromName"]
        if not (isinstance(from_name, str)):
            raise ValueError("Email (IMAP and SMTP) configuration fromName has an invalid type")
        config_arguments['from_name'] = from_name
    else:
        config_arguments['from_name'] = ''
    return Config(**config_arguments)
