# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Brevo Flow that creates or updates one contact by email."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.brevo import (
    Connection,
    ContactWriteResult,
    CreateOrUpdateContactInput,
    new_create_or_update_contact_step,
)

FLOW_TYPE = "BrevoContactSync"
CONNECTION_NAME = "brevo"
_UPSERT_STEP_TYPE = "UpsertBrevoContact"
_OUTCOME = dex.Attribute("brevo-contact-sync-outcome", ContactWriteResult)


@dataclass(frozen=True, slots=True)
class ContactSyncRequest:
    """Contact email and first name to store in Brevo's FNAME attribute."""

    email: str
    first_name: str = field(metadata={"dex_json_name": "firstName"})


def map_to_create_or_update_contact_input(request: ContactSyncRequest) -> CreateOrUpdateContactInput:
    """Upsert the contact with its first name."""
    return CreateOrUpdateContactInput(email=request.email, attributes={"FNAME": request.first_name})


# dex:group group-id:contact-sync group-label:"Contact sync"
# dex:explanation text:"Trim and check the email and first name before saving the contact."
class _Start(dex.Step[ContactSyncRequest]):
    def get_step_type(self) -> str:
        return "StartBrevoContactSync"

    def execute(self, context: dex.Context, input: ContactSyncRequest) -> dex.StepDecision:
        request = ContactSyncRequest(input.email.strip(), input.first_name.strip())
        if not request.email or not request.first_name:
            return dex.force_fail("email and firstName are required")
        return dex.go_to(dex.StepRef(_UPSERT_STEP_TYPE), request)


# dex:group group-id:contact-sync group-label:"Contact sync"
# dex:explanation text:"Record the saved contact and complete the Flow."
class _Saved(dex.Step[MutationResult[ContactWriteResult]]):
    def get_step_type(self) -> str:
        return "BrevoContactSaved"

    def execute(self, context: dex.Context, input: MutationResult[ContactWriteResult]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ContactSyncFlow(dex.Flow[ContactSyncRequest]):
    """Create or update one Brevo contact through the static brevo connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("contact sync requires the static brevo connection")
        self._start = _Start()
        self._saved = _Saved()
        self._upsert = new_create_or_update_contact_step(
            step_type=_UPSERT_STEP_TYPE,
            step_input_type=ContactSyncRequest,
            annotations={
                "groupId": "contact-sync",
                "groupLabel": "Contact sync",
                "explanation": "Create or update the contact by email in Brevo.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_create_or_update_contact_input,
            saved=self._saved,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ContactSyncRequest]:
        """Register the start, contact upsert, and saved Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._upsert, self._saved)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the saved contact result."""
        return dex.PersistenceSchema.of(_OUTCOME)
