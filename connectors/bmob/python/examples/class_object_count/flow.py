# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Count Bmob class objects in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.bmob import (
    Connection,
    CountClassObjectsInput,
    CountClassObjectsOutput,
    new_count_class_objects_step,
)

CONNECTION_NAME = "bmob"
_OUTCOME = dex.Attribute("bmob-class-object-count-outcome", CountClassObjectsOutput)


def map_to_operation_input(input: CountClassObjectsInput) -> CountClassObjectsInput:
    """Pass the class name to the provider Query."""
    return input


# dex:group group-id:count group-label:"Class object count"
# dex:explanation text:"Start counting objects in the requested Bmob class."
class _StartCount(dex.Step[CountClassObjectsInput]):
    def get_step_type(self) -> str:
        return "StartClassObjectCount"

    def execute(self, context: dex.Context, input: CountClassObjectsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CountClassObjectsQuery"), input)


# dex:group group-id:count group-label:"Class object count"
# dex:explanation text:"Persist the class count and complete the Flow."
class _CompleteCount(dex.Step[QueryResult[CountClassObjectsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteClassObjectCount"

    def execute(self, context: dex.Context, input: QueryResult[CountClassObjectsOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ClassObjectCountFlow(dex.Flow[CountClassObjectsInput]):
    """Durably count class objects using the static Bmob connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("class count requires the static Bmob connection")
        self._start = _StartCount()
        self._complete = _CompleteCount()
        self._query = new_count_class_objects_step(
            step_type="CountClassObjectsQuery",
            step_input_type=CountClassObjectsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Bmob",
                "explanation": "Call Bmob countClassObjects with the configured application keys.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            counted=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity shown in Dex Web."""
        return "BmobClassObjectCount"

    def get_steps(self) -> dex.StepList[CountClassObjectsInput]:
        """Register the start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable count Attribute."""
        return dex.PersistenceSchema.of(_OUTCOME)
