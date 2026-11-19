# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable iP138 token usage Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.ip138 import (
    Connection,
    GetTokenUsageInput,
    GetTokenUsageOutput,
    new_get_token_usage_step,
)

CONNECTION_NAME = "ip138"
_RESULT = dex.Attribute("ip138-token-usage", GetTokenUsageOutput)


def map_to_operation_input(request: GetTokenUsageInput) -> GetTokenUsageInput:
    """Forward the empty token-usage request."""
    return request


# dex:group group-id:ip138 group-label:"iP138"
# dex:explanation text:"Start the iP138 token-usage request."
class _StartGetTokenUsage(dex.Step[GetTokenUsageInput]):
    def get_step_type(self) -> str:
        return "StartIP138TokenUsage"

    def execute(self, context: dex.Context, input: GetTokenUsageInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallIP138"), input)


# dex:group group-id:ip138 group-label:"iP138"
# dex:explanation text:"Persist token usage and complete the Flow."
class _CompleteGetTokenUsage(dex.Step[QueryResult[GetTokenUsageOutput]]):
    def get_step_type(self) -> str:
        return "CompleteIP138TokenUsage"

    def execute(self, context: dex.Context, input: QueryResult[GetTokenUsageOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TokenUsageFlow(dex.Flow[GetTokenUsageInput]):
    """Read quota through the static iP138 connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("TokenUsageFlow requires the static iP138 connection")
        self._start = _StartGetTokenUsage()
        self._complete = _CompleteGetTokenUsage()
        self._call = new_get_token_usage_step(
            step_type="CallIP138",
            step_input_type=GetTokenUsageInput,
            annotations={
                "groupId": "ip138",
                "groupLabel": "iP138",
                "explanation": "Read current-hour token use and quota from iP138.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable token-usage Flow identity."""
        return "IP138TokenUsage"

    def get_steps(self) -> dex.StepList[GetTokenUsageInput]:
        """Register the token-usage Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the quota counts."""
        return dex.PersistenceSchema.of(_RESULT)
