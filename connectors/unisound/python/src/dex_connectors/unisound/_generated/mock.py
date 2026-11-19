# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import ListSystemVoicesInput, ListSystemVoicesOutput, SystemVoice
from .definitions import CONNECTOR_ID, DEFECT, FOUND, INVALID_RESPONSE, LIST_SYSTEM_VOICES_DEFINITION, PROVIDER_REJECTED

def list_system_voices_found(value: ListSystemVoicesOutput, failure: Failure | None = None) -> Case[ListSystemVoicesOutput]:
    return Case.branch_result(FOUND, value, failure)

setattr(list_system_voices_found, '__module__', "dex_connectors.unisound.unisoundmock")

def list_system_voices_provider_rejected(value: ListSystemVoicesOutput, failure: Failure | None = None) -> Case[ListSystemVoicesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_system_voices_provider_rejected, '__module__', "dex_connectors.unisound.unisoundmock")

def list_system_voices_invalid_response(value: ListSystemVoicesOutput, failure: Failure | None = None) -> Case[ListSystemVoicesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_system_voices_invalid_response, '__module__', "dex_connectors.unisound.unisoundmock")

def list_system_voices_defect(value: ListSystemVoicesOutput, failure: Failure | None = None) -> Case[ListSystemVoicesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_system_voices_defect, '__module__', "dex_connectors.unisound.unisoundmock")

def list_system_voices_retry(failure: Failure, after_seconds: float = 0) -> Case[ListSystemVoicesOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_system_voices_retry, '__module__', "dex_connectors.unisound.unisoundmock")

def list_system_voices_system_voices() -> Case[ListSystemVoicesOutput]:
    return Case.branch_result(FOUND, ListSystemVoicesOutput(voices=(SystemVoice(voice_id='cn_male_chenyu', name='晨宇', languages=('zh',)),)))

setattr(list_system_voices_system_voices, '__module__', "dex_connectors.unisound.unisoundmock")

def list_system_voices_no_voices() -> Case[ListSystemVoicesOutput]:
    return Case.branch_result(FOUND, ListSystemVoicesOutput(voices=()))

setattr(list_system_voices_no_voices, '__module__', "dex_connectors.unisound.unisoundmock")


class Mock:

    def __init__(self, connection_name: str = "unisound") -> None:
        self.list_system_voices_script: Query[ListSystemVoicesInput, ListSystemVoicesOutput] = Query(LIST_SYSTEM_VOICES_DEFINITION)
        self.list_system_voices_script.default(list_system_voices_system_voices())
        self._connection = new_connection_with_operations(
            list_system_voices=self.list_system_voices_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.unisound.unisoundmock")

__all__ = [
    "Mock",
    "list_system_voices_found",
    "list_system_voices_provider_rejected",
    "list_system_voices_invalid_response",
    "list_system_voices_defect",
    "list_system_voices_retry",
    "list_system_voices_system_voices",
    "list_system_voices_no_voices",
]
