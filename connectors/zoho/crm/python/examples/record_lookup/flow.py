# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Zoho CRM record through the static example connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.zoho_crm import Connection, GetRecordInput, Record, new_get_record_step

CONNECTION_NAME = "zoho-crm"
FLOW_TYPE = "ZohoCRMRecordLookup"


# dex:group group-id:zoho-crm group-label:"Zoho CRM"
# dex:explanation text:"Complete the lookup with the Zoho CRM record returned by the provider."
class _RecordFound(dex.Step[QueryResult[Record]]):
    def get_step_type(self) -> str:
        """Return the stable successful completion Step identity."""
        return "ZohoCRMRecordFound"

    def execute(self, context: dex.Context, input: QueryResult[Record]) -> dex.StepDecision:
        """Complete with the selected Zoho CRM record."""
        return dex.graceful_complete(input.value)


class RecordLookupFlow(dex.Flow[GetRecordInput]):
    """Read a record, failing the Flow when an optional provider outcome is unwired."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Zoho CRM example requires its static connection name")
        self._found = _RecordFound()
        self._get = new_get_record_step(
            step_type="GetZohoCRMRecord",
            step_input_type=GetRecordInput,
            annotations={
                "groupId": "zoho-crm",
                "groupLabel": "Zoho CRM",
                "explanation": "Read one record by its module and numeric Zoho CRM ID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: request,
            found=self._found,
        )

    def get_flow_type(self) -> str:
        """Return the stable example Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetRecordInput]:
        """Register the Zoho CRM read and successful completion Steps."""
        return dex.StepList.start_step(self._get).other_steps(self._found)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist no Attributes or Streams for this read-only Flow."""
        return dex.PersistenceSchema.of()
