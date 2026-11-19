# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List one page of Jiandaoyun applications through a Dex Query."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.fanruan import (
    Connection,
    ListAppsPageInput,
    ListAppsPageOutput,
    new_list_apps_page_step,
)

CONNECTION_NAME = "fanruan"
apps_attribute = dex.Attribute("fanruan-app-page-python", ListAppsPageOutput)


# dex:group group-id:directory group-label:"Application directory"
# dex:explanation text:"Record accessible application names and IDs, then complete the Flow."
class RecordAppsPage(dex.Step[QueryResult[ListAppsPageOutput]]):
    """Persist the non-secret application page and complete the Flow."""

    def get_step_type(self) -> str:
        """Return the stable recording Step identity."""
        return "RecordFanruanAppsPage"

    def execute(self, context: dex.Context, input: QueryResult[ListAppsPageOutput]) -> dex.StepDecision:
        """Record the filtered application metadata."""
        apps_attribute.set(context, input.value)
        return dex.graceful_complete(input.value)


class AppDirectoryFlow(dex.Flow[ListAppsPageInput]):
    """List one page of applications available to the API Key."""

    def __init__(self, connection: Connection) -> None:
        """Bind the read-only Query and completion Step."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("directory Flow requires the static Fanruan connection")
        self.record = RecordAppsPage()
        self.read = new_list_apps_page_step(
            step_type="ListFanruanAppsPage",
            step_input_type=ListAppsPageInput,
            annotations={
                "groupId": "directory",
                "groupLabel": "Application directory",
                "explanation": "POST one page of accessible Jiandaoyun application metadata.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            listed=self.record,
        )

    def get_flow_type(self) -> str:
        """Return the durable Flow identity."""
        return "FanruanAppDirectoryPython"

    def get_steps(self) -> dex.StepList[ListAppsPageInput]:
        """Register provider and recording Steps."""
        return dex.StepList.start_step(self.read).other_steps(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the app-page Attribute."""
        return dex.PersistenceSchema.of(apps_attribute)
