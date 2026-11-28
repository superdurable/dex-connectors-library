# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Personio connection and project credential binding."""

import httpx
from dex_connectors.sdk import ConnectionRef, Query
from dex_connectors.sdk.projectconfig import LoadedProject

from ._generated.connection import Connection as _GeneratedConnection
from ._generated.connection import new_project_connection as _project_connection
from ._generated.definitions import CONNECTOR_ID, GET_PERSON_DEFINITION
from .client import Client
from .models import GetPersonInput, ListPersonsInput, PersonPage, PersonProfile


class Connection(_GeneratedConnection):
    """Bind named Personio operations. Existing single-person bindings may omit the page query."""

    def __init__(
        self,
        reference: ConnectionRef,
        get_person: Query[GetPersonInput, PersonProfile],
        client: Client | None = None,
        *,
        list_persons: Query[ListPersonsInput, PersonPage] | None = None,
    ) -> None:
        if client is not None and list_persons is None:
            list_persons = client.list_persons()
        if list_persons is not None:
            super().__init__(reference, get_person, list_persons, client)
            return
        reference.validate()
        if (
            reference.provider != CONNECTOR_ID
            or get_person is None
            or get_person.definition().operation != GET_PERSON_DEFINITION.operation
        ):
            raise ValueError("Personio connection requires the getPerson operation and provider")
        self.reference = reference
        self._get_person = get_person
        self._client = None

    def list_persons(self) -> Query[ListPersonsInput, PersonPage]:
        """Return the page query, raising ValueError if the caller bound only getPerson."""
        if not hasattr(self, "_list_persons"):
            raise ValueError("listPersons is not bound to this Personio connection")
        return super().list_persons()


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    """Bind both queries and close the client when the connection is closed."""
    if client is None:
        raise ValueError("Personio client is required")
    return Connection(reference, client.get_person(), client)


def new_connection_with_operations(
    get_person: Query[GetPersonInput, PersonProfile],
    reference: ConnectionRef,
    *,
    list_persons: Query[ListPersonsInput, PersonPage] | None = None,
) -> Connection:
    """Bind caller-supplied queries, retaining the two-argument getPerson form."""
    return Connection(reference, get_person, list_persons=list_persons)


class _ProjectConnection(Connection):
    def __init__(self, bound: _GeneratedConnection) -> None:
        super().__init__(bound.reference, bound.get_person(), list_persons=bound.list_persons())
        self._bound = bound

    async def aclose(self) -> None:
        await self._bound.aclose()


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    """Load both queries and renewable credentials for a named Dex project connection."""
    return _ProjectConnection(_project_connection(project, connection_name, http_client))
