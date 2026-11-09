# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Google contact creation Flow using the createContact Mutation."""

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.google_contacts import (
    Connection,
    Contact,
    CreateContactInput,
    new_create_contact_step,
)

FLOW_TYPE = "GoogleContactsCreationPython"
CONNECTION_NAME = "google-contacts"
_CALL_STEP_TYPE = "CallCreateContact"
_RESULT = dex.Attribute("google-created-contact-python", Contact)


def map_to_operation_input(request: CreateContactInput) -> CreateContactInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:google-contacts group-label:"Google Contacts"
# dex:explanation text:"Start the Google Contacts request."
class _StartRequest(dex.Step[CreateContactInput]):
    def get_step_type(self) -> str:
        return "StartCreateContact"

    def execute(self, context: dex.Context, input: CreateContactInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallCreateContact"), input)


# dex:group group-id:google-contacts group-label:"Google Contacts"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[MutationResult[Contact]]):
    def get_step_type(self) -> str:
        return "CompleteCreateContact"

    def execute(self, context: dex.Context, input: MutationResult[Contact]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CreationFlow(dex.Flow[CreateContactInput]):
    """Run one createContact write through the static google-contacts connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("the contact creation Flow requires the static google-contacts connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_create_contact_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=CreateContactInput,
            annotations={
                "groupId": "google-contacts",
                "groupLabel": "Google Contacts",
                "explanation": "Create one Google contact.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            created=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "GoogleContactsCreationPython"

    def get_steps(self) -> dex.StepList[CreateContactInput]:
        """Register the start, provider write, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
