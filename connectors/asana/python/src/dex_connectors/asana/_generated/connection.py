# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_COMMENT_DEFINITION, CREATE_TASK_DEFINITION, GET_TASK_DEFINITION, LIST_TASKS_DEFINITION, UPDATE_TASK_DEFINITION
from ..models import AddCommentInput, AddCommentOutput, CreateTaskInput, CreateTaskOutput, GetTaskInput, ListTasksInput, ListTasksOutput, Task, UpdateTaskInput, UpdateTaskOutput

class Connection:

    __slots__ = ('_list_tasks', '_get_task', '_create_task', '_update_task', '_add_comment', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_tasks: Query[ListTasksInput, ListTasksOutput],
        get_task: Query[GetTaskInput, Task],
        create_task: Mutation[CreateTaskInput, CreateTaskOutput],
        update_task: Mutation[UpdateTaskInput, UpdateTaskOutput],
        add_comment: Mutation[AddCommentInput, AddCommentOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_tasks is None or get_task is None or create_task is None or update_task is None or add_comment is None:
            raise ValueError("Asana connection provider and operations are required")
        if list_tasks.definition().operation != LIST_TASKS_DEFINITION.operation:
            raise ValueError("Asana connection operation identity differs")
        if get_task.definition().operation != GET_TASK_DEFINITION.operation:
            raise ValueError("Asana connection operation identity differs")
        if create_task.definition().operation != CREATE_TASK_DEFINITION.operation:
            raise ValueError("Asana connection operation identity differs")
        if update_task.definition().operation != UPDATE_TASK_DEFINITION.operation:
            raise ValueError("Asana connection operation identity differs")
        if add_comment.definition().operation != ADD_COMMENT_DEFINITION.operation:
            raise ValueError("Asana connection operation identity differs")
        self.reference = reference
        self._list_tasks = list_tasks
        self._get_task = get_task
        self._create_task = create_task
        self._update_task = update_task
        self._add_comment = add_comment
        self._client = client

    def list_tasks(self) -> Query[ListTasksInput, ListTasksOutput]:
        return self._list_tasks

    def get_task(self) -> Query[GetTaskInput, Task]:
        return self._get_task

    def create_task(self) -> Mutation[CreateTaskInput, CreateTaskOutput]:
        return self._create_task

    def update_task(self) -> Mutation[UpdateTaskInput, UpdateTaskOutput]:
        return self._update_task

    def add_comment(self) -> Mutation[AddCommentInput, AddCommentOutput]:
        return self._add_comment

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "asana.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Asana connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.asana.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Asana client is required")
    return Connection(reference, client.list_tasks(), client.get_task(), client.create_task(), client.update_task(), client.add_comment(), client)

setattr(new_connection, '__module__', 'dex_connectors.asana.connection')


def new_connection_with_operations(
    list_tasks: Query[ListTasksInput, ListTasksOutput],
    get_task: Query[GetTaskInput, Task],
    create_task: Mutation[CreateTaskInput, CreateTaskOutput],
    update_task: Mutation[UpdateTaskInput, UpdateTaskOutput],
    add_comment: Mutation[AddCommentInput, AddCommentOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_tasks, get_task, create_task, update_task, add_comment)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.asana.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Asana stored credential does not match the connector credential fields")
        if set(material) - set(('access_token',)):
            raise ValueError("Asana stored credential does not match the connector credential fields")
        access_token = material.get("access_token")
        if access_token is not None and not (isinstance(access_token, str)):
            raise ValueError("Asana credential access_token has an invalid type")
        if not access_token:
            raise ValueError("Asana credential access_token is required")
        return Credentials(SecretString(access_token))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Asana connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Asana project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Asana project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.asana.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"endpoint", "maxResponseBytes"}:
        raise ValueError("Asana configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "endpoint" in value:
        endpoint = value["endpoint"]
        if not (isinstance(endpoint, str)):
            raise ValueError("Asana configuration endpoint has an invalid type")
        config_arguments['endpoint'] = endpoint
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Asana configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
