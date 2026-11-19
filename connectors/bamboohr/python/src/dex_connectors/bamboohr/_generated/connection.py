# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, ADD_EMPLOYEE_DEFINITION, FIND_EMPLOYEE_BY_EMAIL_DEFINITION, GET_EMPLOYEE_DEFINITION, LIST_EMPLOYEE_CHANGES_DEFINITION, LIST_TIME_OFF_REQUESTS_DEFINITION, UPDATE_EMPLOYEE_DEFINITION
from ..models import AddEmployeeInput, AddEmployeeOutput, EmployeeRecord, FindEmployeeByEmailInput, FindEmployeeByEmailOutput, GetEmployeeInput, ListEmployeeChangesInput, ListEmployeeChangesOutput, ListTimeOffRequestsInput, ListTimeOffRequestsOutput, UpdateEmployeeInput, UpdateEmployeeOutput

class Connection:

    __slots__ = ('_get_employee', '_find_employee_by_email', '_list_employee_changes', '_list_time_off_requests', '_update_employee', '_add_employee', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        get_employee: Query[GetEmployeeInput, EmployeeRecord],
        find_employee_by_email: Query[FindEmployeeByEmailInput, FindEmployeeByEmailOutput],
        list_employee_changes: Query[ListEmployeeChangesInput, ListEmployeeChangesOutput],
        list_time_off_requests: Query[ListTimeOffRequestsInput, ListTimeOffRequestsOutput],
        update_employee: Mutation[UpdateEmployeeInput, UpdateEmployeeOutput],
        add_employee: Mutation[AddEmployeeInput, AddEmployeeOutput],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or get_employee is None or find_employee_by_email is None or list_employee_changes is None or list_time_off_requests is None or update_employee is None or add_employee is None:
            raise ValueError("BambooHR connection provider and operations are required")
        if get_employee.definition().operation != GET_EMPLOYEE_DEFINITION.operation:
            raise ValueError("BambooHR connection operation identity differs")
        if find_employee_by_email.definition().operation != FIND_EMPLOYEE_BY_EMAIL_DEFINITION.operation:
            raise ValueError("BambooHR connection operation identity differs")
        if list_employee_changes.definition().operation != LIST_EMPLOYEE_CHANGES_DEFINITION.operation:
            raise ValueError("BambooHR connection operation identity differs")
        if list_time_off_requests.definition().operation != LIST_TIME_OFF_REQUESTS_DEFINITION.operation:
            raise ValueError("BambooHR connection operation identity differs")
        if update_employee.definition().operation != UPDATE_EMPLOYEE_DEFINITION.operation:
            raise ValueError("BambooHR connection operation identity differs")
        if add_employee.definition().operation != ADD_EMPLOYEE_DEFINITION.operation:
            raise ValueError("BambooHR connection operation identity differs")
        self.reference = reference
        self._get_employee = get_employee
        self._find_employee_by_email = find_employee_by_email
        self._list_employee_changes = list_employee_changes
        self._list_time_off_requests = list_time_off_requests
        self._update_employee = update_employee
        self._add_employee = add_employee
        self._client = client

    def get_employee(self) -> Query[GetEmployeeInput, EmployeeRecord]:
        return self._get_employee

    def find_employee_by_email(self) -> Query[FindEmployeeByEmailInput, FindEmployeeByEmailOutput]:
        return self._find_employee_by_email

    def list_employee_changes(self) -> Query[ListEmployeeChangesInput, ListEmployeeChangesOutput]:
        return self._list_employee_changes

    def list_time_off_requests(self) -> Query[ListTimeOffRequestsInput, ListTimeOffRequestsOutput]:
        return self._list_time_off_requests

    def update_employee(self) -> Mutation[UpdateEmployeeInput, UpdateEmployeeOutput]:
        return self._update_employee

    def add_employee(self) -> Mutation[AddEmployeeInput, AddEmployeeOutput]:
        return self._add_employee

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "bamboohr.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("BambooHR connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.bamboohr.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("BambooHR client is required")
    return Connection(reference, client.get_employee(), client.find_employee_by_email(), client.list_employee_changes(), client.list_time_off_requests(), client.update_employee(), client.add_employee(), client)

setattr(new_connection, '__module__', 'dex_connectors.bamboohr.connection')


def new_connection_with_operations(
    get_employee: Query[GetEmployeeInput, EmployeeRecord],
    find_employee_by_email: Query[FindEmployeeByEmailInput, FindEmployeeByEmailOutput],
    list_employee_changes: Query[ListEmployeeChangesInput, ListEmployeeChangesOutput],
    list_time_off_requests: Query[ListTimeOffRequestsInput, ListTimeOffRequestsOutput],
    update_employee: Mutation[UpdateEmployeeInput, UpdateEmployeeOutput],
    add_employee: Mutation[AddEmployeeInput, AddEmployeeOutput],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, get_employee, find_employee_by_email, list_employee_changes, list_time_off_requests, update_employee, add_employee)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.bamboohr.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        api_key = material.get("api_key")
        if not isinstance(api_key, str) or not api_key:
            raise ValueError("BambooHR credential api_key is required")
        return Credentials(SecretString(api_key))


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("BambooHR connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("BambooHR project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("BambooHR project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.bamboohr.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"companyDomain", "maxResponseBytes"}:
        raise ValueError("BambooHR configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "companyDomain" in value:
        company_domain = value["companyDomain"]
        if not (isinstance(company_domain, str)):
            raise ValueError("BambooHR configuration companyDomain has an invalid type")
        config_arguments['company_domain'] = company_domain
    else:
        raise ValueError("BambooHR configuration companyDomain is required")
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("BambooHR configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
