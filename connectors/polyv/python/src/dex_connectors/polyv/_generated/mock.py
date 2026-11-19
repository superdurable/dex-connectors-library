# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListChannelsInput, ListChannelsOutput
from .definitions import CONNECTOR_ID, DEFECT, INVALID_RESPONSE, LISTED, LIST_CHANNELS_DEFINITION, PROVIDER_REJECTED

def list_channels_listed(value: ListChannelsOutput, failure: Failure | None = None) -> Case[ListChannelsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_channels_listed, '__module__', "dex_connectors.polyv.polyvmock")

def list_channels_provider_rejected(value: ListChannelsOutput, failure: Failure | None = None) -> Case[ListChannelsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_channels_provider_rejected, '__module__', "dex_connectors.polyv.polyvmock")

def list_channels_invalid_response(value: ListChannelsOutput, failure: Failure | None = None) -> Case[ListChannelsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_channels_invalid_response, '__module__', "dex_connectors.polyv.polyvmock")

def list_channels_defect(value: ListChannelsOutput, failure: Failure | None = None) -> Case[ListChannelsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_channels_defect, '__module__', "dex_connectors.polyv.polyvmock")

def list_channels_retry(failure: Failure, after_seconds: float = 0) -> Case[ListChannelsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_channels_retry, '__module__', "dex_connectors.polyv.polyvmock")

def list_channels_sample_channels() -> Case[ListChannelsOutput]:
    return Case.branch_result(LISTED, ListChannelsOutput(channel_ids=('2139283', '2139268')))

setattr(list_channels_sample_channels, '__module__', "dex_connectors.polyv.polyvmock")


class Mock:

    def __init__(self, connection_name: str = "polyv") -> None:
        self.list_channels_script: Query[ListChannelsInput, ListChannelsOutput] = Query(LIST_CHANNELS_DEFINITION)
        self.list_channels_script.default(list_channels_sample_channels())
        self._connection = new_connection_with_operations(
            list_channels=self.list_channels_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.polyv.polyvmock")

__all__ = [
    "Mock",
    "list_channels_listed",
    "list_channels_provider_rejected",
    "list_channels_invalid_response",
    "list_channels_defect",
    "list_channels_retry",
    "list_channels_sample_channels",
]
