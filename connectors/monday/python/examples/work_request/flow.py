# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""List, create, update, comment on, and read one monday.com item."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult, QueryResult

from dex_connectors.monday import (
    AddUpdateInput,
    AddUpdateOutput,
    ColumnValue,
    Connection,
    CreateItemInput,
    CreateItemOutput,
    GetItemInput,
    Item,
    ListItemsInput,
    ListItemsOutput,
    UpdateItemColumnValuesInput,
    UpdateItemColumnValuesOutput,
    new_add_update_step,
    new_create_item_step,
    new_get_item_step,
    new_list_items_step,
    new_update_item_column_values_step,
)

CONNECTION_NAME = "monday-work-requests"
FLOW_TYPE = "MondayWorkRequestPython"


@dataclass(frozen=True, slots=True)
class WorkRequest:
    """Safe board and item details supplied by the Flow caller."""

    board_id: str = field(metadata={"dex_json_name": "boardId"})
    item_name: str = field(metadata={"dex_json_name": "itemName"})
    note: str


_REQUEST = dex.Attribute("monday-python-work-request", WorkRequest)


# dex:group group-id:work-request group-label:"Work request"
# dex:explanation text:"Persist the requested board and item name before provider operations."
class RecordWorkRequest(dex.Step[WorkRequest]):
    """Validate and retain the safe start request."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordWorkRequest"

    def execute(self, context: dex.Context, input: WorkRequest) -> dex.StepDecision:
        """Store input and list the board's first item page."""
        if not all((input.board_id, input.item_name, input.note)):
            return dex.force_fail("boardId, itemName, and note are required")
        _REQUEST.set(context, input)
        return dex.go_to(dex.StepRef("ListWorkItems"), input)


# dex:group group-id:work-request group-label:"Work request"
# dex:explanation text:"Create an item after the board's first page has been read."
class WorkItemsListed(dex.Step[QueryResult[ListItemsOutput]]):
    """Route a listed page into item creation."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "WorkItemsListed"

    def execute(self, context: dex.Context, input: QueryResult[ListItemsOutput]) -> dex.StepDecision:
        """Create the requested item on its board."""
        request = _REQUEST.get(context)
        return dex.go_to(
            dex.StepRef("CreateWorkItem"),
            CreateItemInput(
                board_id=request.board_id,
                item_name=request.item_name,
            ),
        )


# dex:group group-id:work-request group-label:"Work request"
# dex:explanation text:"Set an absolute status value on the created item."
class WorkItemCreated(dex.Step[MutationResult[CreateItemOutput]]):
    """Route a confirmed creation into a repeatable column update."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "WorkItemCreated"

    def execute(self, context: dex.Context, input: MutationResult[CreateItemOutput]) -> dex.StepDecision:
        """Set the created item's status column to Done."""
        request = _REQUEST.get(context)
        return dex.go_to(
            dex.StepRef("SetWorkItemStatus"),
            UpdateItemColumnValuesInput(
                board_id=request.board_id,
                item_id=input.value.item_id,
                column_values={"status": ColumnValue(type="status", status_label="Done")},
            ),
        )


# dex:group group-id:work-request group-label:"Work request"
# dex:explanation text:"Add a plain-text note after the item status is set."
class WorkItemUpdated(dex.Step[MutationResult[UpdateItemColumnValuesOutput]]):
    """Route a confirmed column update into an item comment."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "WorkItemUpdated"

    def execute(
        self, context: dex.Context, input: MutationResult[UpdateItemColumnValuesOutput]
    ) -> dex.StepDecision:
        """Comment on the updated item."""
        request = _REQUEST.get(context)
        return dex.go_to(dex.StepRef("AddWorkItemNote"), AddUpdateInput(input.value.item_id, request.note))


# dex:group group-id:work-request group-label:"Work request"
# dex:explanation text:"Read back the item after its note is recorded."
class WorkItemNoted(dex.Step[MutationResult[AddUpdateOutput]]):
    """Route a confirmed comment into a final item read."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "WorkItemNoted"

    def execute(self, context: dex.Context, input: MutationResult[AddUpdateOutput]) -> dex.StepDecision:
        """Read back the item that received the update."""
        return dex.go_to(dex.StepRef("ReadWorkItem"), GetItemInput(input.value.item_id))


# dex:group group-id:work-request group-label:"Work request"
# dex:explanation text:"Return the provider's final item snapshot to the Flow caller."
class WorkItemRead(dex.Step[QueryResult[Item]]):
    """Complete with the final secret-free item."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "WorkItemRead"

    def execute(self, context: dex.Context, input: QueryResult[Item]) -> dex.StepDecision:
        """Complete the Flow with the item snapshot."""
        return dex.graceful_complete(input.value)


class WorkRequestFlow(dex.Flow[WorkRequest]):
    """Run all five monday.com operations through one static connection."""

    def __init__(self, connection: Connection) -> None:
        """Bind the operation Steps before Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("work request Flow requires the monday-work-requests connection")
        self.record = RecordWorkRequest()
        self.listed = WorkItemsListed()
        self.created = WorkItemCreated()
        self.updated = WorkItemUpdated()
        self.noted = WorkItemNoted()
        self.read = WorkItemRead()
        self.list_items = new_list_items_step(
            step_type="ListWorkItems",
            step_input_type=WorkRequest,
            annotations={
                "groupId": "work-request",
                "groupLabel": "Work request",
                "explanation": "Read the board's first item page.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda request: ListItemsInput(board_id=request.board_id),
            listed=self.listed,
        )
        self.create_item = new_create_item_step(
            step_type="CreateWorkItem",
            step_input_type=CreateItemInput,
            annotations={
                "groupId": "work-request",
                "groupLabel": "Work request",
                "explanation": "Create the requested item once.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            created=self.created,
        )
        self.update_item = new_update_item_column_values_step(
            step_type="SetWorkItemStatus",
            step_input_type=UpdateItemColumnValuesInput,
            annotations={
                "groupId": "work-request",
                "groupLabel": "Work request",
                "explanation": "Set the item's status to Done.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            updated=self.updated,
        )
        self.add_update = new_add_update_step(
            step_type="AddWorkItemNote",
            step_input_type=AddUpdateInput,
            annotations={
                "groupId": "work-request",
                "groupLabel": "Work request",
                "explanation": "Add the request's plain-text note.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            added=self.noted,
        )
        self.get_item = new_get_item_step(
            step_type="ReadWorkItem",
            step_input_type=GetItemInput,
            annotations={
                "groupId": "work-request",
                "groupLabel": "Work request",
                "explanation": "Read the final item state.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=lambda input: input,
            found=self.read,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "MondayWorkRequestPython"

    def get_steps(self) -> dex.StepList[WorkRequest]:
        """Register the five operations and their happy-path transitions."""
        return dex.StepList.start_step(self.record).other_steps(
            self.list_items,
            self.listed,
            self.create_item,
            self.created,
            self.update_item,
            self.updated,
            self.add_update,
            self.noted,
            self.get_item,
            self.read,
        )

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the safe request Attribute consumed by later Steps."""
        return dex.PersistenceSchema.of(_REQUEST)
