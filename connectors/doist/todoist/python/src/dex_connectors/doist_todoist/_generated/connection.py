# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CREATE_TASK_DEFINITION, DELETE_TASK_DEFINITION, GET_TASK_DEFINITION, LIST_TASKS_DEFINITION, UPDATE_TASK_CONTENT_DEFINITION
from ..models import CreateTaskInput, DeleteTaskInput, DeleteTaskOutcome, GetTaskInput, ListTasksInput, Task, TaskPage, UpdateTaskInput

class Connection:

    __slots__ = ('_get_task', '_update_task_content', '_list_tasks', '_create_task', '_delete_task', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_task: Query[GetTaskInput, Task],
        update_task_content: Mutation[UpdateTaskInput, Task],
        list_tasks: Query[ListTasksInput, TaskPage],
        create_task: Mutation[CreateTaskInput, Task],
        delete_task: Mutation[DeleteTaskInput, DeleteTaskOutcome],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_task is None or update_task_content is None or list_tasks is None or create_task is None or delete_task is None:
            raise ValueError("Todoist connection provider and operations are required")
        if get_task.definition().operation != GET_TASK_DEFINITION.operation:
            raise ValueError("Todoist connection operation identity differs")
        if update_task_content.definition().operation != UPDATE_TASK_CONTENT_DEFINITION.operation:
            raise ValueError("Todoist connection operation identity differs")
        if list_tasks.definition().operation != LIST_TASKS_DEFINITION.operation:
            raise ValueError("Todoist connection operation identity differs")
        if create_task.definition().operation != CREATE_TASK_DEFINITION.operation:
            raise ValueError("Todoist connection operation identity differs")
        if delete_task.definition().operation != DELETE_TASK_DEFINITION.operation:
            raise ValueError("Todoist connection operation identity differs")
        self.reference = reference
        self._get_task = get_task
        self._update_task_content = update_task_content
        self._list_tasks = list_tasks
        self._create_task = create_task
        self._delete_task = delete_task
        self._client = client

    def get_task(self) -> Query[GetTaskInput, Task]:
        return self._get_task

    def update_task_content(self) -> Mutation[UpdateTaskInput, Task]:
        return self._update_task_content

    def list_tasks(self) -> Query[ListTasksInput, TaskPage]:
        return self._list_tasks

    def create_task(self) -> Mutation[CreateTaskInput, Task]:
        return self._create_task

    def delete_task(self) -> Mutation[DeleteTaskInput, DeleteTaskOutcome]:
        return self._delete_task

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "doist_todoist.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Todoist connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.doist_todoist.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Todoist client is required")
    return Connection(reference, client.get_task(), client.update_task_content(), client.list_tasks(), client.create_task(), client.delete_task(), client)

setattr(new_connection, '__module__', 'dex_connectors.doist_todoist.connection')


def new_connection_with_operations(
    get_task: Query[GetTaskInput, Task],
    update_task_content: Mutation[UpdateTaskInput, Task],
    list_tasks: Query[ListTasksInput, TaskPage],
    create_task: Mutation[CreateTaskInput, Task],
    delete_task: Mutation[DeleteTaskInput, DeleteTaskOutcome],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_task, update_task_content, list_tasks, create_task, delete_task)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.doist_todoist.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_token = material.get("api_token")
        if not isinstance(api_token, str) or not api_token:
            raise ValueError("Todoist credential api_token is required")
        return Credentials(SecretString(api_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Todoist connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Todoist project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Todoist project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.doist_todoist.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"baseUrl", "timeout", "maxResponseBytes"}:
        raise ValueError("Todoist configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "baseUrl" in value:
        base_url = value["baseUrl"]
        if not (isinstance(base_url, str)):
            raise ValueError("Todoist configuration baseUrl has an invalid type")
        config_arguments['base_url'] = base_url
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Todoist configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Todoist configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
