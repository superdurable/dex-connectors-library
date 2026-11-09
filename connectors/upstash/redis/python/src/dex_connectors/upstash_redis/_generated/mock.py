# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Query

from ..connection import Connection, new_connection_with_operations
from ..models import GetKeyInput, KeyLookup
from .definitions import CONNECTOR_ID, DEFECT, FOUND, GET_KEY_DEFINITION, INVALID_RESPONSE, MISSING, PROVIDER_REJECTED

def get_key_found(value: KeyLookup, failure: Failure | None = None) -> Case[KeyLookup]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_key_found, '__module__', "dex_connectors.upstash_redis.redismock")

def get_key_missing(value: KeyLookup, failure: Failure | None = None) -> Case[KeyLookup]:
    return Case.branch_result(MISSING, value, failure)

setattr(get_key_missing, '__module__', "dex_connectors.upstash_redis.redismock")

def get_key_provider_rejected(value: KeyLookup, failure: Failure | None = None) -> Case[KeyLookup]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_key_provider_rejected, '__module__', "dex_connectors.upstash_redis.redismock")

def get_key_invalid_response(value: KeyLookup, failure: Failure | None = None) -> Case[KeyLookup]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_key_invalid_response, '__module__', "dex_connectors.upstash_redis.redismock")

def get_key_defect(value: KeyLookup, failure: Failure | None = None) -> Case[KeyLookup]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_key_defect, '__module__', "dex_connectors.upstash_redis.redismock")

def get_key_retry(failure: Failure, after_seconds: float = 0) -> Case[KeyLookup]:
    return Case.retry(failure, after_seconds)

setattr(get_key_retry, '__module__', "dex_connectors.upstash_redis.redismock")

def get_key_found_default() -> Case[KeyLookup]:
    return Case.branch_result(FOUND, KeyLookup(key='sample:key', value='example', exists=True))

setattr(get_key_found_default, '__module__', "dex_connectors.upstash_redis.redismock")


class Mock:

    def __init__(self, connection_name: str = "upstash-redis") -> None:
        self.get_key_script: Query[GetKeyInput, KeyLookup] = Query(GET_KEY_DEFINITION)
        self.get_key_script.default(get_key_found_default())
        self._connection = new_connection_with_operations(
            get_key=self.get_key_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.upstash_redis.redismock")

__all__ = [
    "Mock",
    "get_key_found",
    "get_key_missing",
    "get_key_provider_rejected",
    "get_key_invalid_response",
    "get_key_defect",
    "get_key_retry",
    "get_key_found_default",
]
