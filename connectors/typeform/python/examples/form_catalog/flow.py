# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List the connected Typeform account's forms through a Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.typeform import Connection, FormPage, ListFormsInput, new_list_forms_step

CONNECTION_NAME = "typeform-forms"
FLOW_TYPE = "TypeformFormCatalogPython"


@dataclass(frozen=True, slots=True)
class FormCatalogRequest:
    """Select a workspace and page of forms."""

    workspace_id: str = field(default="", metadata={"dex_json_name": "workspaceId"})
    page: int = 1

    def to_operation_input(self) -> ListFormsInput:
        """Map the Flow start value into the provider query."""
        return ListFormsInput(workspace_id=self.workspace_id, page=self.page)


catalog_result = dex.Attribute("typeform-form-catalog-result", FormPage)


# dex:group group-id:form-catalog group-label:"Form catalog"
# dex:explanation text:"Persist the listed form page and complete the Flow."
class CompleteCatalog(dex.Step[QueryResult[FormPage]]):
    """Complete with one safe form page."""

    def execute(self, context: dex.Context, input: QueryResult[FormPage]) -> dex.StepDecision:
        """Store and return the Typeform form page."""
        catalog_result.set(context, input.value)
        return dex.graceful_complete(input.value)


class FormCatalogFlow(dex.Flow[FormCatalogRequest]):
    """List one page of forms using the static Typeform connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("form catalog requires the static Typeform connection")
        self.complete = CompleteCatalog()
        self.list_forms = new_list_forms_step(
            step_type="ListTypeformForms",
            step_input_type=FormCatalogRequest,
            annotations={
                "groupId": "form-catalog",
                "groupLabel": "Form catalog",
                "explanation": "List a page of forms from the connected Typeform account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=FormCatalogRequest.to_operation_input,
            listed=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[FormCatalogRequest]:
        """Register the provider query and completion Step."""
        return dex.StepList.start_step(self.list_forms).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the saved form page Attribute."""
        return dex.PersistenceSchema.of(catalog_result)
