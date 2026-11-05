# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Zoho CRM record with a named Dex Web connection."""

import dex
from dex_connectors.sdk import QueryResult, QueryStep

from dex_connectors.zoho_crm import Connection, GetRecordInput, Record, new_get_record_step

from .models import CONNECTION_NAME, RecordLookupInput, RecordLookupOutput


# dex:group group-id:zoho-crm group-label:"Zoho CRM"
# dex:explanation text:"Complete after Zoho CRM confirms the requested record exists."
class RecordFound(dex.Step[QueryResult[Record]]):
    """Complete with the provider-confirmed record ID."""

    async def execute(self, context: dex.AsyncContext, input: QueryResult[Record]) -> dex.StepDecision:  # type: ignore[override]
        """Return the confirmed record identity without provider credentials."""
        return dex.graceful_complete(RecordLookupOutput(input.value.module, input.value.id))


# dex:group group-id:zoho-crm group-label:"Zoho CRM"
# dex:explanation text:"Validate the lookup before asking Zoho CRM for the record."
class PrepareLookup(dex.Step[RecordLookupInput]):
    """Validate and dispatch the named record read."""

    async def execute(self, context: dex.AsyncContext, input: RecordLookupInput) -> dex.StepDecision:  # type: ignore[override]
        """Pass the module and record ID to the connector Step."""
        if not input.module.strip() or not input.record_id.strip():
            return dex.force_fail("module and record ID are required")
        return dex.go_to(dex.StepRef("ReadZohoRecord"), input)


class RecordLookupFlow(dex.Flow[RecordLookupInput]):
    """Read one record through the static zoho-crm-main connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Zoho CRM Flow requires the static zoho-crm-main connection")
        self.prepare = PrepareLookup()
        self.found = RecordFound()
        self.read: QueryStep[RecordLookupInput, GetRecordInput, Record] = new_get_record_step(
            step_type="ReadZohoRecord",
            step_input_type=RecordLookupInput,
            annotations={
                "groupId": "zoho-crm",
                "groupLabel": "Zoho CRM",
                "explanation": "Read the record by module and ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda value: GetRecordInput(value.module, value.record_id),
            found=self.found,
        )

    def get_steps(self) -> dex.StepList[RecordLookupInput]:
        """Register each Step once under its stable type."""
        return dex.StepList.start_step(self.prepare).other_steps(self.read, self.found)
