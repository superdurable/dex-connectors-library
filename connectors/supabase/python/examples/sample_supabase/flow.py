# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Supabase sample Flow using the selectRows Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.supabase import (
    Connection,
    SelectRowsInput,
    SelectRowsOutput,
    new_select_rows_step,
)

FLOW_TYPE = "SupabaseSample"
CONNECTION_NAME = "supabase"
_CALL_STEP_TYPE = "CallSupabase"
_RESULT = dex.Attribute("supabase-sample-result", SelectRowsOutput)


def map_to_operation_input(request: SelectRowsInput) -> SelectRowsInput:
    """Pass the validated Flow request to the connector."""
    return request


# dex:group group-id:supabase-start group-label:"Supabase sample"
# dex:explanation text:"Pass the start input to the connector operation."
class _StartRequest(dex.Step[SelectRowsInput]):
    def get_step_type(self) -> str:
        return "StartSupabaseRequest"

    def execute(self, context: dex.Context, input: SelectRowsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef(_CALL_STEP_TYPE), input)


# dex:group group-id:supabase-complete group-label:"Supabase sample"
# dex:explanation text:"Complete the Flow with the provider response."
class _CompleteRequest(dex.Step[QueryResult[SelectRowsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteSupabaseRequest"

    def execute(self, context: dex.Context, input: QueryResult[SelectRowsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class SampleFlow(dex.Flow[SelectRowsInput]):
    """Run one selectRows read through the static supabase connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the sample requires the static supabase connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_select_rows_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=SelectRowsInput,
            annotations={
                "groupId": "supabase",
                "groupLabel": "Supabase",
                "explanation": "Select a bounded page of table rows through PostgREST.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            selected=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[SelectRowsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
