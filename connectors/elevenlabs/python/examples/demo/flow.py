# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable ElevenLabs demo Flow using the listVoices Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.elevenlabs import Connection, ListVoicesInput, ListVoicesOutput, new_list_voices_step

FLOW_TYPE = "ElevenLabsDemo"
CONNECTION_NAME = "elevenlabs"
_OPERATION_STEP_TYPE = "CallListVoices"
_OUTCOME = dex.Attribute("elevenlabs-demo-outcome", ListVoicesOutput)


def map_to_operation_input(input: ListVoicesInput) -> ListVoicesInput:
    """Request the voice page unchanged."""
    return input


# dex:group group-id:request group-label:"Request"
# dex:explanation text:"Pass the typed start input to the provider Step."
class _StartRequest(dex.Step[ListVoicesInput]):
    def get_step_type(self) -> str:
        return "StartListVoices"

    def execute(self, context: dex.Context, input: ListVoicesInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_OPERATION_STEP_TYPE), input)


# dex:group group-id:result group-label:"Result"
# dex:explanation text:"Complete the Flow with the provider's typed result."
class _CompleteResult(dex.Step[QueryResult[ListVoicesOutput]]):
    def get_step_type(self) -> str:
        return "CompleteListVoices"

    def execute(self, context: dex.Context, input: QueryResult[ListVoicesOutput]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class DemoFlow(dex.Flow[ListVoicesInput]):
    """List voices through the static elevenlabs connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("demo requires the static elevenlabs connection")
        self._start = _StartRequest()
        self._complete = _CompleteResult()
        self._read = new_list_voices_step(
            step_type=_OPERATION_STEP_TYPE,
            step_input_type=ListVoicesInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "ElevenLabs",
                "explanation": "Call the provider's listVoices operation.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ListVoicesInput]:
        """Register the start, voice listing, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._read, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the voice page."""
        return dex.PersistenceSchema.of(_OUTCOME)
