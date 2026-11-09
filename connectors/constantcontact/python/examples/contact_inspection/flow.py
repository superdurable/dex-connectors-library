# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Constant Contact contact inspection Flow using getContact."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.constantcontact import (
    Connection,
    ContactMetadata,
    GetContactInput,
    new_get_contact_step,
)

FLOW_TYPE = "ConstantContactContactInspectionPython"
CONNECTION_NAME = "constantcontact"
_CALL_STEP_TYPE = "CallConstantContact"
_RESULT = dex.Attribute("constantcontact-contact", ContactMetadata)


def map_to_operation_input(request: GetContactInput) -> GetContactInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:constantcontact group-label:"Constant Contact"
# dex:explanation text:"Start the Constant Contact request."
class _StartRequest(dex.Step[GetContactInput]):
    def get_step_type(self) -> str:
        return "StartConstantContactRequest"

    def execute(self, context: dex.Context, input: GetContactInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallConstantContact"), input)


# dex:group group-id:constantcontact group-label:"Constant Contact"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[ContactMetadata]]):
    def get_step_type(self) -> str:
        return "CompleteConstantContactRequest"

    def execute(self, context: dex.Context, input: QueryResult[ContactMetadata]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactInspectionFlow(dex.Flow[GetContactInput]):
    """Read one contact through the static constantcontact connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact inspection requires the static constantcontact connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_get_contact_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=GetContactInput,
            annotations={
                "groupId": "constantcontact",
                "groupLabel": "Constant Contact",
                "explanation": "Read one contact from the connected Constant Contact account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            found=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "ConstantContactContactInspectionPython"

    def get_steps(self) -> dex.StepList[GetContactInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
