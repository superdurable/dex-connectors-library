# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString, CredentialRefreshDriver
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject
from dex_connectors.sdk.projectconfig.provider import RefreshingCredentialProvider
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_MEETING_DEFINITION, GET_MEETING_DEFINITION, LIST_MEETINGS_DEFINITION, LIST_PAST_MEETING_PARTICIPANTS_DEFINITION, UPDATE_MEETING_DEFINITION
from .refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from ..models import CreateMeetingInput, GetMeetingInput, ListMeetingsInput, ListPastMeetingParticipantsInput, Meeting, MeetingPage, ParticipantPage, UpdateMeetingInput, UpdatedMeeting

class Connection:

    __slots__ = ('_list_meetings', '_get_meeting', '_create_meeting', '_update_meeting', '_list_past_meeting_participants', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_meetings: Query[ListMeetingsInput, MeetingPage],
        get_meeting: Query[GetMeetingInput, Meeting],
        create_meeting: Mutation[CreateMeetingInput, Meeting],
        update_meeting: Mutation[UpdateMeetingInput, UpdatedMeeting],
        list_past_meeting_participants: Query[ListPastMeetingParticipantsInput, ParticipantPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_meetings is None or get_meeting is None or create_meeting is None or update_meeting is None or list_past_meeting_participants is None:
            raise ValueError("Zoom connection provider and operations are required")
        if list_meetings.definition().operation != LIST_MEETINGS_DEFINITION.operation:
            raise ValueError("Zoom connection operation identity differs")
        if get_meeting.definition().operation != GET_MEETING_DEFINITION.operation:
            raise ValueError("Zoom connection operation identity differs")
        if create_meeting.definition().operation != CREATE_MEETING_DEFINITION.operation:
            raise ValueError("Zoom connection operation identity differs")
        if update_meeting.definition().operation != UPDATE_MEETING_DEFINITION.operation:
            raise ValueError("Zoom connection operation identity differs")
        if list_past_meeting_participants.definition().operation != LIST_PAST_MEETING_PARTICIPANTS_DEFINITION.operation:
            raise ValueError("Zoom connection operation identity differs")
        self.reference = reference
        self._list_meetings = list_meetings
        self._get_meeting = get_meeting
        self._create_meeting = create_meeting
        self._update_meeting = update_meeting
        self._list_past_meeting_participants = list_past_meeting_participants
        self._client = client

    def list_meetings(self) -> Query[ListMeetingsInput, MeetingPage]:
        return self._list_meetings

    def get_meeting(self) -> Query[GetMeetingInput, Meeting]:
        return self._get_meeting

    def create_meeting(self) -> Mutation[CreateMeetingInput, Meeting]:
        return self._create_meeting

    def update_meeting(self) -> Mutation[UpdateMeetingInput, UpdatedMeeting]:
        return self._update_meeting

    def list_past_meeting_participants(self) -> Query[ListPastMeetingParticipantsInput, ParticipantPage]:
        return self._list_past_meeting_participants

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "zoom.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Zoom connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.zoom.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Zoom client is required")
    return Connection(reference, client.list_meetings(), client.get_meeting(), client.create_meeting(), client.update_meeting(), client.list_past_meeting_participants(), client)

setattr(new_connection, '__module__', 'dex_connectors.zoom.connection')


def new_connection_with_operations(
    list_meetings: Query[ListMeetingsInput, MeetingPage],
    get_meeting: Query[GetMeetingInput, Meeting],
    create_meeting: Mutation[CreateMeetingInput, Meeting],
    update_meeting: Mutation[UpdateMeetingInput, UpdatedMeeting],
    list_past_meeting_participants: Query[ListPastMeetingParticipantsInput, ParticipantPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_meetings, get_meeting, create_meeting, update_meeting, list_past_meeting_participants)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.zoom.connection')


def decode_credentials(material: dict[str, Any], *, shared: bool = False) -> Credentials:
    if not isinstance(material, dict):
        raise ValueError("Zoom stored credential does not match the connector credential fields")
    if set(material) - set(('oauth_client_id', 'oauth_client_secret', 'access_token', 'refresh_token')):
        raise ValueError("Zoom stored credential does not match the connector credential fields")
    is_shared = shared
    oauth_client_id = material.get("oauth_client_id")
    if oauth_client_id is not None and not (isinstance(oauth_client_id, str)):
        raise ValueError("Zoom credential oauth_client_id has an invalid type")
    oauth_client_secret = material.get("oauth_client_secret")
    if oauth_client_secret is not None and not (isinstance(oauth_client_secret, str)):
        raise ValueError("Zoom credential oauth_client_secret has an invalid type")
    access_token = material.get("access_token")
    if access_token is not None and not (isinstance(access_token, str)):
        raise ValueError("Zoom credential access_token has an invalid type")
    refresh_token = material.get("refresh_token")
    if refresh_token is not None and not (isinstance(refresh_token, str)):
        raise ValueError("Zoom credential refresh_token has an invalid type")
    if not oauth_client_id:
        raise ValueError("Zoom credential oauth_client_id is required")
    if not oauth_client_secret and not is_shared:
        raise ValueError("Zoom credential oauth_client_secret is required")
    if not access_token:
        raise ValueError("Zoom credential access_token is required")
    if not refresh_token and not is_shared:
        raise ValueError("Zoom credential refresh_token is required")
    return Credentials(oauth_client_id, SecretString(oauth_client_secret or ''), SecretString(access_token), SecretString(refresh_token or ''))


def decode_shared_credentials(material: dict[str, Any]) -> Credentials:
    return decode_credentials(material, shared=True)


def encode_credentials(credentials: Credentials) -> dict[str, Any]:
    material: dict[str, Any] = {}
    if credentials.oauth_client_id:
        material["oauth_client_id"] = credentials.oauth_client_id
    if credentials.oauth_client_secret.reveal():
        material["oauth_client_secret"] = credentials.oauth_client_secret.reveal()
    if credentials.access_token.reveal():
        material["access_token"] = credentials.access_token.reveal()
    if credentials.refresh_token.reveal():
        material["refresh_token"] = credentials.refresh_token.reveal()
    decode_credentials(material)
    return material


class _ProjectCredentialSource:
    def __init__(self, project: LoadedProject, key: ConnectionKey, http_client: httpx.AsyncClient, settings: dict[str, Any]) -> None:
        self.provider = RefreshingCredentialProvider(
            project.connections, key, decode_credentials, encode_credentials, decode_shared_credentials
        )
        self.driver = ManifestCredentialRefreshDriver[Credentials]("Zoom", AUTHORIZATION, CREDENTIAL_FIELDS, http_client, settings=settings)

    async def resolve(self, call: Call) -> Credentials:
        return await self.provider.resolve_with_refresh(call, self.driver)

    async def resolve_with_refresh(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_with_refresh(call, driver or self.driver)

    async def resolve_after_rejection(self, call: Call, driver: CredentialRefreshDriver[Credentials] | None = None) -> Credentials:
        return await self.provider.resolve_after_rejection(call, driver or self.driver)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Zoom connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Zoom project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Zoom project connection provider differs")
    config = _decode_config(matching[0].configuration)
    http_client = http_client or httpx.AsyncClient()
    client = Client(config, _ProjectCredentialSource(project, key, http_client, matching[0].configuration), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.zoom.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("Zoom configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Zoom configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Zoom configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
