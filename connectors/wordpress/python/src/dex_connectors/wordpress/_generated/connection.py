# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_POST_DEFINITION, GET_POST_DEFINITION, LIST_POSTS_DEFINITION
from ..models import CreatePostInput, CreatedPost, GetPostInput, ListPostsInput, Post, PostPage

class Connection:

    __slots__ = ('_create_post', '_get_post', '_list_posts', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        create_post: Mutation[CreatePostInput, CreatedPost],
        get_post: Query[GetPostInput, Post],
        list_posts: Query[ListPostsInput, PostPage],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or create_post is None or get_post is None or list_posts is None:
            raise ValueError("WordPress connection provider and operations are required")
        if create_post.definition().operation != CREATE_POST_DEFINITION.operation:
            raise ValueError("WordPress connection operation identity differs")
        if get_post.definition().operation != GET_POST_DEFINITION.operation:
            raise ValueError("WordPress connection operation identity differs")
        if list_posts.definition().operation != LIST_POSTS_DEFINITION.operation:
            raise ValueError("WordPress connection operation identity differs")
        self.reference = reference
        self._create_post = create_post
        self._get_post = get_post
        self._list_posts = list_posts
        self._client = client

    def create_post(self) -> Mutation[CreatePostInput, CreatedPost]:
        return self._create_post

    def get_post(self) -> Query[GetPostInput, Post]:
        return self._get_post

    def list_posts(self) -> Query[ListPostsInput, PostPage]:
        return self._list_posts

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "wordpress.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("WordPress connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.wordpress.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("WordPress client is required")
    return Connection(reference, client.create_post(), client.get_post(), client.list_posts(), client)

setattr(new_connection, '__module__', 'dex_connectors.wordpress.connection')


def new_connection_with_operations(
    create_post: Mutation[CreatePostInput, CreatedPost],
    get_post: Query[GetPostInput, Post],
    list_posts: Query[ListPostsInput, PostPage],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, create_post, get_post, list_posts)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.wordpress.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("WordPress stored credential does not match the connector credential fields")
        if set(material) - set(('username', 'application_password')):
            raise ValueError("WordPress stored credential does not match the connector credential fields")
        username = material.get("username")
        if username is not None and not (isinstance(username, str)):
            raise ValueError("WordPress credential username has an invalid type")
        application_password = material.get("application_password")
        if application_password is not None and not (isinstance(application_password, str)):
            raise ValueError("WordPress credential application_password has an invalid type")
        if not username:
            raise ValueError("WordPress credential username is required")
        if not application_password:
            raise ValueError("WordPress credential application_password is required")
        return Credentials(username, SecretString(application_password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("WordPress connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("WordPress project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("WordPress project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.wordpress.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"siteUrl", "timeout", "maxResponseBytes", "maxPostBytes"}:
        raise ValueError("WordPress configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "siteUrl" in value:
        site_url = value["siteUrl"]
        if not (isinstance(site_url, str)):
            raise ValueError("WordPress configuration siteUrl has an invalid type")
        config_arguments['site_url'] = site_url
    else:
        raise ValueError("WordPress configuration siteUrl is required")
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("WordPress configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("WordPress configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "maxPostBytes" in value:
        max_post_bytes = value["maxPostBytes"]
        if not (type(max_post_bytes) is int):
            raise ValueError("WordPress configuration maxPostBytes has an invalid type")
        config_arguments['max_post_bytes'] = max_post_bytes
    return Config(**config_arguments)
