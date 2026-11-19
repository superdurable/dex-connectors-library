# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, LIST_ACCESSIBLE_PROJECTS_DEFINITION
from ..models import ListAccessibleProjectsInput, ListAccessibleProjectsOutput

class Connection:

    __slots__ = ('_list_accessible_projects', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        list_accessible_projects: Query[ListAccessibleProjectsInput, ListAccessibleProjectsOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or list_accessible_projects is None:
            raise ValueError("Huawei Cloud IAM connection provider and operations are required")
        if list_accessible_projects.definition().operation != LIST_ACCESSIBLE_PROJECTS_DEFINITION.operation:
            raise ValueError("Huawei Cloud IAM connection operation identity differs")
        self.reference = reference
        self._list_accessible_projects = list_accessible_projects
        self._client = client

    def list_accessible_projects(self) -> Query[ListAccessibleProjectsInput, ListAccessibleProjectsOutput]:
        return self._list_accessible_projects

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "huaweicloud.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Huawei Cloud IAM connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.huaweicloud.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Huawei Cloud IAM client is required")
    return Connection(reference, client.list_accessible_projects(), client)

setattr(new_connection, '__module__', 'dex_connectors.huaweicloud.connection')


def new_connection_with_operations(
    list_accessible_projects: Query[ListAccessibleProjectsInput, ListAccessibleProjectsOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, list_accessible_projects)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.huaweicloud.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        if not isinstance(material, dict):
            raise ValueError("Huawei Cloud IAM stored credential does not match the connector credential fields")
        if set(material) - set(('account_name', 'iam_username', 'iam_password')):
            raise ValueError("Huawei Cloud IAM stored credential does not match the connector credential fields")
        account_name = material.get("account_name")
        if account_name is not None and not (isinstance(account_name, str)):
            raise ValueError("Huawei Cloud IAM credential account_name has an invalid type")
        iam_username = material.get("iam_username")
        if iam_username is not None and not (isinstance(iam_username, str)):
            raise ValueError("Huawei Cloud IAM credential iam_username has an invalid type")
        iam_password = material.get("iam_password")
        if iam_password is not None and not (isinstance(iam_password, str)):
            raise ValueError("Huawei Cloud IAM credential iam_password has an invalid type")
        if not account_name:
            raise ValueError("Huawei Cloud IAM credential account_name is required")
        if not iam_username:
            raise ValueError("Huawei Cloud IAM credential iam_username is required")
        if not iam_password:
            raise ValueError("Huawei Cloud IAM credential iam_password is required")
        return Credentials(SecretString(account_name), SecretString(iam_username), SecretString(iam_password))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Huawei Cloud IAM connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Huawei Cloud IAM project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Huawei Cloud IAM project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.huaweicloud.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"timeout", "maxResponseBytes"}:
        raise ValueError("Huawei Cloud IAM configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "timeout" in value:
        timeout = value["timeout"]
        if not (type(timeout) is int):
            raise ValueError("Huawei Cloud IAM configuration timeout has an invalid type")
        config_arguments['timeout_seconds'] = timeout / 1_000_000_000
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Huawei Cloud IAM configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
