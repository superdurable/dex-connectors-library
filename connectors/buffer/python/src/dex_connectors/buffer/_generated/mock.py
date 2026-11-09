# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ChannelProfile, GetChannelInput
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_CHANNEL_DEFINITION, INVALID_RESPONSE, NOT_FOUND, PROVIDER_REJECTED

def get_channel_found(value: ChannelProfile, failure: Failure | None = None) -> Case[ChannelProfile]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_channel_found, '__module__', "dex_connectors.buffer.buffermock")

def get_channel_not_found(value: ChannelProfile, failure: Failure | None = None) -> Case[ChannelProfile]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_channel_not_found, '__module__', "dex_connectors.buffer.buffermock")

def get_channel_provider_rejected(value: ChannelProfile, failure: Failure | None = None) -> Case[ChannelProfile]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_channel_provider_rejected, '__module__', "dex_connectors.buffer.buffermock")

def get_channel_invalid_response(value: ChannelProfile, failure: Failure | None = None) -> Case[ChannelProfile]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_channel_invalid_response, '__module__', "dex_connectors.buffer.buffermock")

def get_channel_defect(value: ChannelProfile, failure: Failure | None = None) -> Case[ChannelProfile]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_channel_defect, '__module__', "dex_connectors.buffer.buffermock")

def get_channel_retry(failure: Failure, after_seconds: float = 0) -> Case[ChannelProfile]:
    return Case.retry(failure, after_seconds)

setattr(get_channel_retry, '__module__', "dex_connectors.buffer.buffermock")

def get_channel_found_default() -> Case[ChannelProfile]:
    return Case.branch_result(FOUND, ChannelProfile(id='channel-1', name='Example Channel', service='instagram'))

setattr(get_channel_found_default, '__module__', "dex_connectors.buffer.buffermock")


class Mock:

    def __init__(self, connection_name: str = "buffer") -> None:
        self.get_channel_script: Query[GetChannelInput, ChannelProfile] = Query(GET_CHANNEL_DEFINITION)
        self.get_channel_script.default(get_channel_found_default())
        self._connection = new_connection_with_operations(
            get_channel=self.get_channel_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.buffer.buffermock")

__all__ = [
    "Mock",
    "get_channel_found",
    "get_channel_not_found",
    "get_channel_provider_rejected",
    "get_channel_invalid_response",
    "get_channel_defect",
    "get_channel_retry",
    "get_channel_found_default",
]
