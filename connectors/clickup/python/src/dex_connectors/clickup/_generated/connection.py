# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_COMMENT_DEFINITION, CREATE_TASK_DEFINITION, FIND_MEMBER_BY_EMAIL_DEFINITION, GET_TASK_DEFINITION, SEARCH_TASKS_DEFINITION, UPDATE_TASK_DEFINITION, UPDATE_TASK_TAGS_DEFINITION
from ..models import AddCommentInput, AddCommentOutput, CreateTaskInput, CreateTaskOutput, FindMemberByEmailInput, GetTaskInput, Member, SearchTasksInput, SearchTasksOutput, Task, UpdateTaskInput, UpdateTaskOutput, UpdateTaskTagsInput, UpdateTaskTagsOutput

class Connection:

    __slots__ = ('_search_tasks', '_get_task', '_create_task', '_update_task', '_update_task_tags', '_add_comment', '_find_member_by_email', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        search_tasks: Query[SearchTasksInput, SearchTasksOutput],
        get_task: Query[GetTaskInput, Task],
        create_task: Mutation[CreateTaskInput, CreateTaskOutput],
        update_task: Mutation[UpdateTaskInput, UpdateTaskOutput],
        update_task_tags: Mutation[UpdateTaskTagsInput, UpdateTaskTagsOutput],
        add_comment: Mutation[AddCommentInput, AddCommentOutput],
        find_member_by_email: Query[FindMemberByEmailInput, Member],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or search_tasks is None or get_task is None or create_task is None or update_task is None or update_task_tags is None or add_comment is None or find_member_by_email is None:
            raise ValueError("ClickUp connection provider and operations are required")
        if search_tasks.definition().operation != SEARCH_TASKS_DEFINITION.operation:
            raise ValueError("ClickUp connection operation identity differs")
        if get_task.definition().operation != GET_TASK_DEFINITION.operation:
            raise ValueError("ClickUp connection operation identity differs")
        if create_task.definition().operation != CREATE_TASK_DEFINITION.operation:
            raise ValueError("ClickUp connection operation identity differs")
        if update_task.definition().operation != UPDATE_TASK_DEFINITION.operation:
            raise ValueError("ClickUp connection operation identity differs")
        if update_task_tags.definition().operation != UPDATE_TASK_TAGS_DEFINITION.operation:
            raise ValueError("ClickUp connection operation identity differs")
        if add_comment.definition().operation != ADD_COMMENT_DEFINITION.operation:
            raise ValueError("ClickUp connection operation identity differs")
        if find_member_by_email.definition().operation != FIND_MEMBER_BY_EMAIL_DEFINITION.operation:
            raise ValueError("ClickUp connection operation identity differs")
        self.reference = reference
        self._search_tasks = search_tasks
        self._get_task = get_task
        self._create_task = create_task
        self._update_task = update_task
        self._update_task_tags = update_task_tags
        self._add_comment = add_comment
        self._find_member_by_email = find_member_by_email
        self._client = client

    def search_tasks(self) -> Query[SearchTasksInput, SearchTasksOutput]:
        return self._search_tasks

    def get_task(self) -> Query[GetTaskInput, Task]:
        return self._get_task

    def create_task(self) -> Mutation[CreateTaskInput, CreateTaskOutput]:
        return self._create_task

    def update_task(self) -> Mutation[UpdateTaskInput, UpdateTaskOutput]:
        return self._update_task

    def update_task_tags(self) -> Mutation[UpdateTaskTagsInput, UpdateTaskTagsOutput]:
        return self._update_task_tags

    def add_comment(self) -> Mutation[AddCommentInput, AddCommentOutput]:
        return self._add_comment

    def find_member_by_email(self) -> Query[FindMemberByEmailInput, Member]:
        return self._find_member_by_email

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "clickup.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("ClickUp connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.clickup.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("ClickUp client is required")
    return Connection(reference, client.search_tasks(), client.get_task(), client.create_task(), client.update_task(), client.update_task_tags(), client.add_comment(), client.find_member_by_email(), client)

setattr(new_connection, '__module__', 'dex_connectors.clickup.connection')


def new_connection_with_operations(
    search_tasks: Query[SearchTasksInput, SearchTasksOutput],
    get_task: Query[GetTaskInput, Task],
    create_task: Mutation[CreateTaskInput, CreateTaskOutput],
    update_task: Mutation[UpdateTaskInput, UpdateTaskOutput],
    update_task_tags: Mutation[UpdateTaskTagsInput, UpdateTaskTagsOutput],
    add_comment: Mutation[AddCommentInput, AddCommentOutput],
    find_member_by_email: Query[FindMemberByEmailInput, Member],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, search_tasks, get_task, create_task, update_task, update_task_tags, add_comment, find_member_by_email)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.clickup.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("ClickUp stored credential does not match the connector credential fields")
        if set(material) - set(('api_token', 'webhook_secret')):
            raise ValueError("ClickUp stored credential does not match the connector credential fields")
        api_token = material.get("api_token")
        if api_token is not None and not (isinstance(api_token, str)):
            raise ValueError("ClickUp credential api_token has an invalid type")
        webhook_secret = material.get("webhook_secret")
        if webhook_secret is not None and not (isinstance(webhook_secret, str)):
            raise ValueError("ClickUp credential webhook_secret has an invalid type")
        if not api_token:
            raise ValueError("ClickUp credential api_token is required")
        return Credentials(SecretString(api_token), SecretString(webhook_secret or ''))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("ClickUp connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("ClickUp project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("ClickUp project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.clickup.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"maxResponseBytes", "webhookMaxBodyBytes"}:
        raise ValueError("ClickUp configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "maxResponseBytes" in value and value["maxResponseBytes"] != '':
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("ClickUp configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    if "webhookMaxBodyBytes" in value and value["webhookMaxBodyBytes"] != '':
        webhook_max_body_bytes = value["webhookMaxBodyBytes"]
        if not (type(webhook_max_body_bytes) is int):
            raise ValueError("ClickUp configuration webhookMaxBodyBytes has an invalid type")
        config_arguments['webhook_max_body_bytes'] = webhook_max_body_bytes
    return Config(**config_arguments)
