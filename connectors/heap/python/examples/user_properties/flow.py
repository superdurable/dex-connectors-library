# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Set one user's absolute Heap properties through a static connection."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.heap import (
    Connection,
    SetUserPropertiesInput,
    UserPropertiesResult,
    new_set_user_properties_step,
)

FLOW_TYPE = "HeapUserPropertiesPython"
CONNECTION_NAME = "heap"
_OPERATION_STEP = "SetHeapUserProperties"
_OUTCOME = dex.Attribute("heap-user-properties-outcome", UserPropertiesResult)


def map_to_operation_input(input: SetUserPropertiesInput) -> SetUserPropertiesInput:
    """Preserve the typed identity and property values for the Mutation."""
    return input


# dex:group group-id:user-properties group-label:"User properties"
# dex:explanation text:"Pass the typed identity and properties to Heap."
class _Start(dex.Step[SetUserPropertiesInput]):
    def get_step_type(self) -> str:
        return "StartHeapUserPropertiesPython"

    def execute(self, context: dex.Context, input: SetUserPropertiesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("SetHeapUserProperties"), input)


# dex:group group-id:user-properties group-label:"User properties"
# dex:explanation text:"Record provider acceptance and complete the Flow."
class _Accepted(dex.Step[MutationResult[UserPropertiesResult]]):
    def get_step_type(self) -> str:
        return "HeapUserPropertiesAcceptedPython"

    def execute(self, context: dex.Context, input: MutationResult[UserPropertiesResult]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class UserPropertiesFlow(dex.Flow[SetUserPropertiesInput]):
    """Send one bounded property update to Heap and persist the acceptance result."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("user properties require the static heap connection")
        self._start = _Start()
        self._accepted = _Accepted()
        self._operation = new_set_user_properties_step(
            step_type=_OPERATION_STEP,
            step_input_type=SetUserPropertiesInput,
            annotations={
                "groupId": "user-properties",
                "groupLabel": "User properties",
                "explanation": "Replace selected properties for one stable Heap identity.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            accepted=self._accepted,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "HeapUserPropertiesPython"

    def get_steps(self) -> dex.StepList[SetUserPropertiesInput]:
        """Register the start, provider, and accepted Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._operation, self._accepted)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider acceptance result."""
        return dex.PersistenceSchema.of(_OUTCOME)
