# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Upstash Redis key lookup Flow using getKey."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.upstash_redis import (
    Connection,
    GetKeyInput,
    KeyLookup,
    new_get_key_step,
)

FLOW_TYPE = "UpstashRedisKeyLookupPython"
CONNECTION_NAME = "upstash-redis"
_CALL_STEP_TYPE = "CallGetKey"
_RESULT = dex.Attribute("redis-keys", KeyLookup)


def map_to_operation_input(request: GetKeyInput) -> GetKeyInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:redis group-label:"Upstash Redis"
# dex:explanation text:"Start the Upstash Redis request."
class _StartRequest(dex.Step[GetKeyInput]):
    def get_step_type(self) -> str:
        return "StartGetKeyRequest"

    def execute(self, context: dex.Context, input: GetKeyInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallGetKey"), input)


# dex:group group-id:redis group-label:"Upstash Redis"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[KeyLookup]]):
    def get_step_type(self) -> str:
        return "CompleteGetKeyRequest"

    def execute(self, context: dex.Context, input: QueryResult[KeyLookup]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class KeyLookupFlow(dex.Flow[GetKeyInput]):
    """Read one key value through the static upstash-redis connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("key lookup requires the static upstash-redis connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_key_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetKeyInput,
            annotations={
                "groupId": "redis",
                "groupLabel": "Upstash Redis",
                "explanation": "Read one key value from the connected Upstash Redis account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "UpstashRedisKeyLookupPython"

    def get_steps(self) -> dex.StepList[GetKeyInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
