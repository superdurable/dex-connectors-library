# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one Sensors Data provider result in a durable Dex Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.sensorsdata import (
    Connection,
    ListCampaignsInput,
    ListCampaignsOutput,
    new_list_campaigns_step,
)

CONNECTION_NAME = "sensorsdata"
_RESULT = dex.Attribute("sensorsdata-campaign-catalog", ListCampaignsOutput)


def map_to_operation_input(input: ListCampaignsInput) -> ListCampaignsInput:
    """Pass the public Flow input to the provider Query."""
    return input


# dex:group group-id:sensorsdata group-label:"Sensors Data"
# dex:explanation text:"Start the Sensors Data Query for this Flow."
class _Start(dex.Step[ListCampaignsInput]):
    def get_step_type(self) -> str:
        return "StartCampaignCatalogFlow"

    def execute(self, context: dex.Context, input: ListCampaignsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("ListCampaignsQuery"), input)


# dex:group group-id:sensorsdata group-label:"Sensors Data"
# dex:explanation text:"Persist the validated provider result and complete the Flow."
class _Complete(dex.Step[QueryResult[ListCampaignsOutput]]):
    def get_step_type(self) -> str:
        return "CompleteCampaignCatalog"

    def execute(self, context: dex.Context, input: QueryResult[ListCampaignsOutput]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class CampaignCatalogFlow(dex.Flow[ListCampaignsInput]):
    """Call Sensors Data through the static named connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("Sensors Data connection name differs")
        self._start = _Start()
        self._complete = _Complete()
        self._query = new_list_campaigns_step(
            step_type="ListCampaignsQuery",
            step_input_type=ListCampaignsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Sensors Data",
                "explanation": "Read one channel campaign page for the configured project.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable application Flow identity."""
        return "SensorsDataCampaignCatalogFlow"

    def get_steps(self) -> dex.StepList[ListCampaignsInput]:
        """Register start, provider Query, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._query, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the durable provider result Attribute."""
        return dex.PersistenceSchema.of(_RESULT)
