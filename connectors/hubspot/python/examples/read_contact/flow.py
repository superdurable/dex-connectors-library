# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one HubSpot contact through the operation-specific Dex Step."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.hubspot import Connection, CRMObject, GetObjectInput, new_get_object_step

CONNECTION_NAME = "hubspot-crm"
FLOW_TYPE = "HubSpotReadContactPython"
contact_record = dex.Attribute("hubspot-contact-record", CRMObject)


# dex:group group-id:hubspot-contact group-label:"HubSpot Contact"
# dex:explanation text:"Persist the returned contact without credentials."
class CompleteContact(dex.Step[QueryResult[CRMObject]]):
    """Persist a safe CRM object and finish the Flow."""

    def execute(self, context: dex.Context, input: QueryResult[CRMObject]) -> dex.StepDecision:
        """Save the record in the Flow's declared persistence schema."""
        contact_record.set(context, input.value)
        return dex.graceful_complete(input.value)


class ReadContactFlow(dex.Flow[GetObjectInput]):
    """Read a contact by numeric HubSpot ID."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static HubSpot connection at Worker registration."""
        self.complete = CompleteContact()
        self.read = new_get_object_step(
            step_type="GetHubSpotContact",
            step_input_type=GetObjectInput,
            annotations={
                "groupId": "hubspot-contact",
                "groupLabel": "HubSpot Contact",
                "explanation": "Read one HubSpot contact by record ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: value,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetObjectInput]:
        """Route the successful provider result to completion."""
        return dex.StepList.start_step(self.read).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the safe contact Attribute."""
        return dex.PersistenceSchema.of(contact_record)
