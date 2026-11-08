# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Keap contact lookup Flow using getContact."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.keap import (
    Connection,
    ContactProfile,
    GetContactInput,
    new_get_contact_step,
)

FLOW_TYPE = "KeapContactReadPython"
CONNECTION_NAME = "keap"
_CALL_STEP_TYPE = "CallKeap"
_RESULT = dex.Attribute("keap-contact-read", ContactProfile)


def map_to_operation_input(request: GetContactInput) -> GetContactInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:keap group-label:"Keap"
# dex:explanation text:"Start the Keap request."
class _StartRequest(dex.Step[GetContactInput]):
    def get_step_type(self) -> str:
        return "StartKeapRequest"

    def execute(self, context: dex.Context, input: GetContactInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallKeap"), input)


# dex:group group-id:keap group-label:"Keap"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ContactProfile]]):
    def get_step_type(self) -> str:
        return "CompleteKeapRequest"

    def execute(self, context: dex.Context, input: QueryResult[ContactProfile]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactReadFlow(dex.Flow[GetContactInput]):
    """Read contact profile through the static keap connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact lookup requires the static keap connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_contact_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetContactInput,
            annotations={
                "groupId": "keap",
                "groupLabel": "Keap",
                "explanation": "Read contact profile from the connected Keap CRM.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "KeapContactReadPython"

    def get_steps(self) -> dex.StepList[GetContactInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
