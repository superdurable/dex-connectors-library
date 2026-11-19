# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, GET_MEMBER_DEFINITION, LIST_MEMBERS_DEFINITION, SEND_CAMPAIGN_DEFINITION, UPDATE_MEMBER_TAGS_DEFINITION, UPSERT_MEMBER_DEFINITION
from ..models import GetMemberInput, ListMembersInput, ListMembersOutput, Member, SendCampaignInput, SendCampaignOutput, UpdateMemberTagsInput, UpdateMemberTagsOutput, UpsertMemberInput, UpsertMemberOutput

class Connection:

    __slots__ = ('_get_member', '_list_members', '_upsert_member', '_update_member_tags', '_send_campaign', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_member: Query[GetMemberInput, Member],
        list_members: Query[ListMembersInput, ListMembersOutput],
        upsert_member: Mutation[UpsertMemberInput, UpsertMemberOutput],
        update_member_tags: Mutation[UpdateMemberTagsInput, UpdateMemberTagsOutput],
        send_campaign: Mutation[SendCampaignInput, SendCampaignOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_member is None or list_members is None or upsert_member is None or update_member_tags is None or send_campaign is None:
            raise ValueError("Mailchimp connection provider and operations are required")
        if get_member.definition().operation != GET_MEMBER_DEFINITION.operation:
            raise ValueError("Mailchimp connection operation identity differs")
        if list_members.definition().operation != LIST_MEMBERS_DEFINITION.operation:
            raise ValueError("Mailchimp connection operation identity differs")
        if upsert_member.definition().operation != UPSERT_MEMBER_DEFINITION.operation:
            raise ValueError("Mailchimp connection operation identity differs")
        if update_member_tags.definition().operation != UPDATE_MEMBER_TAGS_DEFINITION.operation:
            raise ValueError("Mailchimp connection operation identity differs")
        if send_campaign.definition().operation != SEND_CAMPAIGN_DEFINITION.operation:
            raise ValueError("Mailchimp connection operation identity differs")
        self.reference = reference
        self._get_member = get_member
        self._list_members = list_members
        self._upsert_member = upsert_member
        self._update_member_tags = update_member_tags
        self._send_campaign = send_campaign
        self._client = client

    def get_member(self) -> Query[GetMemberInput, Member]:
        return self._get_member

    def list_members(self) -> Query[ListMembersInput, ListMembersOutput]:
        return self._list_members

    def upsert_member(self) -> Mutation[UpsertMemberInput, UpsertMemberOutput]:
        return self._upsert_member

    def update_member_tags(self) -> Mutation[UpdateMemberTagsInput, UpdateMemberTagsOutput]:
        return self._update_member_tags

    def send_campaign(self) -> Mutation[SendCampaignInput, SendCampaignOutput]:
        return self._send_campaign

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "intuit_mailchimp.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Mailchimp connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.intuit_mailchimp.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Mailchimp client is required")
    return Connection(reference, client.get_member(), client.list_members(), client.upsert_member(), client.update_member_tags(), client.send_campaign(), client)

setattr(new_connection, '__module__', 'dex_connectors.intuit_mailchimp.connection')


def new_connection_with_operations(
    get_member: Query[GetMemberInput, Member],
    list_members: Query[ListMembersInput, ListMembersOutput],
    upsert_member: Mutation[UpsertMemberInput, UpsertMemberOutput],
    update_member_tags: Mutation[UpdateMemberTagsInput, UpdateMemberTagsOutput],
    send_campaign: Mutation[SendCampaignInput, SendCampaignOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_member, list_members, upsert_member, update_member_tags, send_campaign)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.intuit_mailchimp.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("Mailchimp credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Mailchimp connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Mailchimp project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Mailchimp project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.intuit_mailchimp.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes"}:
        raise ValueError("Mailchimp configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Mailchimp configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
