# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Salesforce Contact through a named Dex connection."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.salesforce import (
    Connection,
    GetRecordInput,
    Record,
    new_get_record_step,
)

CONNECTION_NAME = "salesforce-crm"


# dex:group group-id:salesforce-lookup group-label:"Salesforce record lookup"
# dex:explanation text:"Complete with the record Salesforce returned."
class RecordRead(dex.Step[QueryResult[Record]]):
    """Finish the Flow after a definite record read."""

    async def execute(self, context: dex.AsyncContext, input: QueryResult[Record]) -> dex.StepDecision:  # type: ignore[override]
        """Return the non-secret record as the Flow result."""
        return dex.graceful_complete(input.value)


def map_to_get_record_input(value: GetRecordInput) -> GetRecordInput:
    """Pass the typed request directly to the connector operation."""
    return value


class RecordLookupFlow(dex.Flow[GetRecordInput]):
    """Look up a Salesforce record with the static salesforce-crm connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("record lookup requires the salesforce-crm connection")
        self.read = RecordRead()
        self.get_record = new_get_record_step(
            step_type="ReadSalesforceRecord",
            step_input_type=GetRecordInput,
            annotations={
                "groupId": "salesforce-lookup",
                "groupLabel": "Salesforce record lookup",
                "explanation": "Read the selected Salesforce record.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_record_input,
            found=self.read,
        )

    def get_flow_type(self) -> str:
        """Return the stable Salesforce example Flow identity."""
        return "SalesforceRecordLookup"

    def get_steps(self) -> dex.StepList[GetRecordInput]:
        """Register the connector Step and its completion target."""
        return dex.StepList.start_step(self.get_record).other_steps(self.read)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Keep record values in Step Results without custom persistence fields."""
        return dex.PersistenceSchema.of()
