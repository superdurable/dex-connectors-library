# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from typing import Any

import httpx
from dex_connectors.sdk import ConnectionRef, Mutation, Query, Call, SecretString
from dex_connectors.sdk.projectconfig import ConnectionKey, LoadedProject

from ..client import Client, Config, Credentials
from .definitions import CONNECTOR_ID, CANCEL_STATEMENT_DEFINITION, GET_STATEMENT_RESULT_DEFINITION, SUBMIT_STATEMENT_DEFINITION
from ..models import CancelStatementInput, GetStatementResultInput, StatementCancellation, StatementResult, StatementSubmission, SubmitStatementInput

class Connection:

    __slots__ = ('_submit_statement', '_get_statement_result', '_cancel_statement', '_client', 'reference')

    def __init__(self,
        reference: ConnectionRef,
        submit_statement: Mutation[SubmitStatementInput, StatementSubmission],
        get_statement_result: Query[GetStatementResultInput, StatementResult],
        cancel_statement: Mutation[CancelStatementInput, StatementCancellation],
        client: Client | None = None,
    ) -> None:
        reference.validate()
        if reference.provider != CONNECTOR_ID or submit_statement is None or get_statement_result is None or cancel_statement is None:
            raise ValueError("Snowflake connection provider and operations are required")
        if submit_statement.definition().operation != SUBMIT_STATEMENT_DEFINITION.operation:
            raise ValueError("Snowflake connection operation identity differs")
        if get_statement_result.definition().operation != GET_STATEMENT_RESULT_DEFINITION.operation:
            raise ValueError("Snowflake connection operation identity differs")
        if cancel_statement.definition().operation != CANCEL_STATEMENT_DEFINITION.operation:
            raise ValueError("Snowflake connection operation identity differs")
        self.reference = reference
        self._submit_statement = submit_statement
        self._get_statement_result = get_statement_result
        self._cancel_statement = cancel_statement
        self._client = client

    def submit_statement(self) -> Mutation[SubmitStatementInput, StatementSubmission]:
        return self._submit_statement

    def get_statement_result(self) -> Query[GetStatementResultInput, StatementResult]:
        return self._get_statement_result

    def cancel_statement(self) -> Mutation[CancelStatementInput, StatementCancellation]:
        return self._cancel_statement

    async def aclose(self) -> None:
        if self._client is not None:
            await self._client.aclose()

    def __repr__(self) -> str:
        return "snowflake.Connection([REDACTED])"

    def __reduce__(self) -> tuple[object, ...]:
        raise TypeError("Snowflake connections cannot be serialized")

setattr(Connection, '__module__', 'dex_connectors.snowflake.connection')


def new_connection(client: Client, reference: ConnectionRef) -> Connection:
    if client is None:
        raise ValueError("Snowflake client is required")
    return Connection(reference, client.submit_statement(), client.get_statement_result(), client.cancel_statement(), client)

setattr(new_connection, '__module__', 'dex_connectors.snowflake.connection')


def new_connection_with_operations(
    submit_statement: Mutation[SubmitStatementInput, StatementSubmission],
    get_statement_result: Query[GetStatementResultInput, StatementResult],
    cancel_statement: Mutation[CancelStatementInput, StatementCancellation],
    reference: ConnectionRef,
) -> Connection:
    return Connection(reference, submit_statement, get_statement_result, cancel_statement)

setattr(new_connection_with_operations, '__module__', 'dex_connectors.snowflake.connection')


class _ProjectCredentialSource:

    def __init__(self, project: LoadedProject, key: ConnectionKey) -> None:
        self.project = project
        self.key = key

    async def resolve(self, call: Call) -> Credentials:
        material = await self.project.resolve_credential(self.key, str(call.id))
        selected_method = material.get('auth_method')
        if selected_method is None:
            selected_method = ''
        if not isinstance(selected_method, str):
            raise ValueError("Snowflake credential auth_method is invalid")
        method = selected_method or "key-pair"
        values: dict[str, Any] = {'auth_method_id': method}
        if method == "key-pair":
            user = material.get("user")
            if user is not None and not isinstance(user, str):
                raise ValueError("Snowflake credential user has an invalid type")
            if not user:
                raise ValueError("Snowflake credential user is required")
            if user:
                values["user"] = user
            private_key = material.get("private_key")
            if private_key is not None and not isinstance(private_key, str):
                raise ValueError("Snowflake credential private_key has an invalid type")
            if not private_key:
                raise ValueError("Snowflake credential private_key is required")
            if private_key:
                values["private_key"] = SecretString(private_key)
        elif method == "programmatic-access-token":
            programmatic_access_token = material.get("programmatic_access_token")
            if programmatic_access_token is not None and not isinstance(programmatic_access_token, str):
                raise ValueError("Snowflake credential programmatic_access_token has an invalid type")
            if not programmatic_access_token:
                raise ValueError("Snowflake credential programmatic_access_token is required")
            if programmatic_access_token:
                values["programmatic_access_token"] = SecretString(programmatic_access_token)
        else:
            raise ValueError("Snowflake credential auth_method is invalid")
        return Credentials(**values)


def new_project_connection(
    project: LoadedProject, connection_name: str, http_client: httpx.AsyncClient | None = None
) -> Connection:
    if project is None:
        raise ValueError("Snowflake connection requires loaded project configuration")
    reference = ConnectionRef(CONNECTOR_ID, connection_name)
    reference.validate()
    key = ConnectionKey(CONNECTOR_ID, connection_name)
    matching = [
        entry
        for entry in project.configuration.connections
        if entry.connector_id == CONNECTOR_ID and entry.connection_name == connection_name
    ]
    if len(matching) != 1:
        raise ValueError("Snowflake project connection is missing or duplicated")
    if matching[0].provider != CONNECTOR_ID:
        raise ValueError("Snowflake project connection provider differs")
    config = _decode_config(matching[0].configuration)
    client = Client(config, _ProjectCredentialSource(project, key), http_client)
    return new_connection(client, reference)

setattr(new_project_connection, '__module__', 'dex_connectors.snowflake.connection')


def _decode_config(value: dict[str, Any]) -> Config:
    if set(value) - {"accountIdentifier", "warehouse", "role", "database", "schema", "statementTimeoutSeconds", "maxRows", "maxResponseBytes"}:
        raise ValueError("Snowflake configuration has unknown fields")
    config_arguments: dict[str, Any] = {}
    if "accountIdentifier" in value:
        account_identifier = value["accountIdentifier"]
        if not (isinstance(account_identifier, str)):
            raise ValueError("Snowflake configuration accountIdentifier has an invalid type")
        config_arguments['account_identifier'] = account_identifier
    else:
        raise ValueError("Snowflake configuration accountIdentifier is required")
    if "warehouse" in value:
        warehouse = value["warehouse"]
        if not (isinstance(warehouse, str)):
            raise ValueError("Snowflake configuration warehouse has an invalid type")
        config_arguments['warehouse'] = warehouse
    else:
        config_arguments['warehouse'] = ''
    if "role" in value:
        role = value["role"]
        if not (isinstance(role, str)):
            raise ValueError("Snowflake configuration role has an invalid type")
        config_arguments['role'] = role
    else:
        config_arguments['role'] = ''
    if "database" in value:
        database = value["database"]
        if not (isinstance(database, str)):
            raise ValueError("Snowflake configuration database has an invalid type")
        config_arguments['database'] = database
    else:
        config_arguments['database'] = ''
    if "schema" in value:
        schema = value["schema"]
        if not (isinstance(schema, str)):
            raise ValueError("Snowflake configuration schema has an invalid type")
        config_arguments['schema'] = schema
    else:
        config_arguments['schema'] = ''
    if "statementTimeoutSeconds" in value:
        statement_timeout_seconds = value["statementTimeoutSeconds"]
        if not (type(statement_timeout_seconds) is int):
            raise ValueError("Snowflake configuration statementTimeoutSeconds has an invalid type")
        config_arguments['statement_timeout_seconds'] = statement_timeout_seconds
    if "maxRows" in value:
        max_rows = value["maxRows"]
        if not (type(max_rows) is int):
            raise ValueError("Snowflake configuration maxRows has an invalid type")
        config_arguments['max_rows'] = max_rows
    if "maxResponseBytes" in value:
        max_response_bytes = value["maxResponseBytes"]
        if not (type(max_response_bytes) is int):
            raise ValueError("Snowflake configuration maxResponseBytes has an invalid type")
        config_arguments['max_response_bytes'] = max_response_bytes
    return Config(**config_arguments)
