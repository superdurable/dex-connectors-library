# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ClientMetadata, ClientsList, ListClientsInput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_CLIENTS_DEFINITION, PROVIDER_REJECTED

def list_clients_listed(value: ClientsList, failure: Failure | None = None) -> Case[ClientsList]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_clients_listed, '__module__', "dex_connectors.campaignmonitor.campaignmonitormock")

def list_clients_provider_rejected(value: ClientsList, failure: Failure | None = None) -> Case[ClientsList]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_clients_provider_rejected, '__module__', "dex_connectors.campaignmonitor.campaignmonitormock")

def list_clients_invalid_response(value: ClientsList, failure: Failure | None = None) -> Case[ClientsList]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_clients_invalid_response, '__module__', "dex_connectors.campaignmonitor.campaignmonitormock")

def list_clients_defect(value: ClientsList, failure: Failure | None = None) -> Case[ClientsList]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_clients_defect, '__module__', "dex_connectors.campaignmonitor.campaignmonitormock")

def list_clients_retry(failure: Failure, after_seconds: float = 0) -> Case[ClientsList]:
    return Case.retry(failure, after_seconds)

setattr(list_clients_retry, '__module__', "dex_connectors.campaignmonitor.campaignmonitormock")

def list_clients_listed_default() -> Case[ClientsList]:
    return Case.branch_result(LISTED, ClientsList(clients=(ClientMetadata(id='sample-client-id', name='Sample Client'),)))

setattr(list_clients_listed_default, '__module__', "dex_connectors.campaignmonitor.campaignmonitormock")


class Mock:

    def __init__(self, connection_name: str = "campaignmonitor") -> None:
        self.list_clients_script: Query[ListClientsInput, ClientsList] = Query(LIST_CLIENTS_DEFINITION)
        self.list_clients_script.default(list_clients_listed_default())
        self._connection = new_connection_with_operations(
            list_clients=self.list_clients_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.campaignmonitor.campaignmonitormock")

__all__ = [
    "Mock",
    "list_clients_listed",
    "list_clients_provider_rejected",
    "list_clients_invalid_response",
    "list_clients_defect",
    "list_clients_retry",
    "list_clients_listed_default",
]
