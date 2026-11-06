# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import AddUpdateInput, AddUpdateOutput, ColumnValue, CreateItemInput, CreateItemOutput, GetItemInput, Item, ItemColumnValue, ItemFilter, ItemFilterRule, ItemGroup, ItemOrder, ListItemsInput, ListItemsOutput, UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput
from .definitions import CONNECTOR_ID, ADDED, ADD_UPDATE_DEFINITION, CREATED, CREATE_ITEM_DEFINITION, DEFECT, FOUND, GET_ITEM_DEFINITION, INVALID_RESPONSE, LISTED, LIST_ITEMS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, UNCERTAIN, UPDATED, UPDATE_ITEM_COLUMN_VALUES_DEFINITION

def list_items_listed(value: ListItemsOutput, failure: Failure | None = None) -> Case[ListItemsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_items_listed, '__module__', "dex_connectors.monday.mondaymock")

def list_items_not_found(value: ListItemsOutput, failure: Failure | None = None) -> Case[ListItemsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_items_not_found, '__module__', "dex_connectors.monday.mondaymock")

def list_items_provider_rejected(value: ListItemsOutput, failure: Failure | None = None) -> Case[ListItemsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_items_provider_rejected, '__module__', "dex_connectors.monday.mondaymock")

def list_items_invalid_response(value: ListItemsOutput, failure: Failure | None = None) -> Case[ListItemsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_items_invalid_response, '__module__', "dex_connectors.monday.mondaymock")

def list_items_defect(value: ListItemsOutput, failure: Failure | None = None) -> Case[ListItemsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_items_defect, '__module__', "dex_connectors.monday.mondaymock")

def list_items_retry(failure: Failure, after_seconds: float = 0) -> Case[ListItemsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_items_retry, '__module__', "dex_connectors.monday.mondaymock")

def list_items_completed_work_request_pages() -> Case[ListItemsOutput]:
    return Case.paged(LISTED, (ListItemsOutput(board_id='1234567890', items=(Item(id='5550002', name='Monthly Fire Drill Checklist - February', state='active', board_id='1234567890', column_values=(ItemColumnValue(id='status', type='status', text='Done'),)),), next_cursor='completed-page-2'), ListItemsOutput(board_id='1234567890', items=(Item(id='5550002', name='Monthly Fire Drill Checklist - February', state='active', board_id='1234567890', column_values=(ItemColumnValue(id='status', type='status', text='Done'),)), Item(id='5550003', name='Monthly Fire Drill Checklist - February', state='active', board_id='1234567890', column_values=(ItemColumnValue(id='status', type='status', text='Done'),))), next_cursor='completed-page-3'), ListItemsOutput(board_id='1234567890', items=())))

setattr(list_items_completed_work_request_pages, '__module__', "dex_connectors.monday.mondaymock")

def list_items_not_found_failure() -> Case[ListItemsOutput]:
    return Case.branch_result(NOT_FOUND, ListItemsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listItems", "The board does not exist or the connection cannot see it."))

setattr(list_items_not_found_failure, '__module__', "dex_connectors.monday.mondaymock")

def list_items_provider_rejected_failure() -> Case[ListItemsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListItemsOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "listItems", "monday.com rejected the list filter column."))

setattr(list_items_provider_rejected_failure, '__module__', "dex_connectors.monday.mondaymock")

def list_items_invalid_response_failure() -> Case[ListItemsOutput]:
    return Case.branch_result(INVALID_RESPONSE, ListItemsOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "listItems", "monday.com returned a malformed page of items."))

setattr(list_items_invalid_response_failure, '__module__', "dex_connectors.monday.mondaymock")

def get_item_found(value: Item, failure: Failure | None = None) -> Case[Item]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_item_found, '__module__', "dex_connectors.monday.mondaymock")

def get_item_not_found(value: Item, failure: Failure | None = None) -> Case[Item]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_item_not_found, '__module__', "dex_connectors.monday.mondaymock")

def get_item_provider_rejected(value: Item, failure: Failure | None = None) -> Case[Item]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_item_provider_rejected, '__module__', "dex_connectors.monday.mondaymock")

def get_item_invalid_response(value: Item, failure: Failure | None = None) -> Case[Item]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_item_invalid_response, '__module__', "dex_connectors.monday.mondaymock")

def get_item_defect(value: Item, failure: Failure | None = None) -> Case[Item]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_item_defect, '__module__', "dex_connectors.monday.mondaymock")

def get_item_retry(failure: Failure, after_seconds: float = 0) -> Case[Item]:
    return Case.retry(failure, after_seconds)

setattr(get_item_retry, '__module__', "dex_connectors.monday.mondaymock")

def get_item_created_work_item() -> Case[Item]:
    return Case.branch_result(FOUND, Item(id='5550004', name='Monthly Fire Drill Checklist - February', state='active', board_id='1234567890', group=ItemGroup(id='february', title='February'), column_values=(ItemColumnValue(id='status', type='status', text='Working on it'), ItemColumnValue(id='date4', type='date', text='2026-02-18'))))

setattr(get_item_created_work_item, '__module__', "dex_connectors.monday.mondaymock")

def get_item_not_found_failure() -> Case[Item]:
    return Case.branch_result(NOT_FOUND, Item(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getItem", "The item does not exist, the connection cannot see it, or it is archived or deleted and inactive items were not requested."))

setattr(get_item_not_found_failure, '__module__', "dex_connectors.monday.mondaymock")

def get_item_provider_rejected_failure() -> Case[Item]:
    return Case.branch_result(PROVIDER_REJECTED, Item(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getItem", "monday.com denied item read permission."))

setattr(get_item_provider_rejected_failure, '__module__', "dex_connectors.monday.mondaymock")

def get_item_invalid_response_failure() -> Case[Item]:
    return Case.branch_result(INVALID_RESPONSE, Item(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "getItem", "monday.com returned a malformed item."))

setattr(get_item_invalid_response_failure, '__module__', "dex_connectors.monday.mondaymock")

def create_item_created(value: CreateItemOutput, failure: Failure | None = None) -> Case[CreateItemOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_item_created, '__module__', "dex_connectors.monday.mondaymock")

def create_item_not_found(value: CreateItemOutput, failure: Failure | None = None) -> Case[CreateItemOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(create_item_not_found, '__module__', "dex_connectors.monday.mondaymock")

def create_item_provider_rejected(value: CreateItemOutput, failure: Failure | None = None) -> Case[CreateItemOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_item_provider_rejected, '__module__', "dex_connectors.monday.mondaymock")

def create_item_uncertain(value: CreateItemOutput, failure: Failure) -> Case[CreateItemOutput]:
    return Case.uncertain(value, failure)

setattr(create_item_uncertain, '__module__', "dex_connectors.monday.mondaymock")

def create_item_defect(value: CreateItemOutput, failure: Failure | None = None) -> Case[CreateItemOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_item_defect, '__module__', "dex_connectors.monday.mondaymock")

def create_item_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateItemOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_item_retry, '__module__', "dex_connectors.monday.mondaymock")

def create_item_february_work_item() -> Case[CreateItemOutput]:
    return Case.branch_result(CREATED, CreateItemOutput(board_id='1234567890', group_id='february', item_name='Monthly Fire Drill Checklist - February', item_id='5550004', item=Item(id='5550004', name='Monthly Fire Drill Checklist - February', state='active', board_id='1234567890', group=ItemGroup(id='february', title='February'), column_values=(ItemColumnValue(id='status', type='status', text='Working on it'), ItemColumnValue(id='date4', type='date', text='2026-02-18')))))

setattr(create_item_february_work_item, '__module__', "dex_connectors.monday.mondaymock")

def create_item_not_found_failure() -> Case[CreateItemOutput]:
    return Case.branch_result(NOT_FOUND, CreateItemOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "createItem", "The board or group does not exist or the connection cannot see it; nothing was created."))

setattr(create_item_not_found_failure, '__module__', "dex_connectors.monday.mondaymock")

def create_item_provider_rejected_failure() -> Case[CreateItemOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateItemOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "createItem", "monday.com rejected the status label."))

setattr(create_item_provider_rejected_failure, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_updated(value: UpdateItemColumnValuesOutput, failure: Failure | None = None) -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_item_column_values_updated, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_not_found(value: UpdateItemColumnValuesOutput, failure: Failure | None = None) -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_item_column_values_not_found, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_provider_rejected(value: UpdateItemColumnValuesOutput, failure: Failure | None = None) -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_item_column_values_provider_rejected, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_invalid_response(value: UpdateItemColumnValuesOutput, failure: Failure | None = None) -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_item_column_values_invalid_response, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_defect(value: UpdateItemColumnValuesOutput, failure: Failure | None = None) -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_item_column_values_defect, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateItemColumnValuesOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_item_column_values_retry, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_scheduled_work_item() -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(UPDATED, UpdateItemColumnValuesOutput(board_id='1234567890', item_id='5550004', item=Item(id='5550004', name='Monthly Fire Drill Checklist - February', state='active', board_id='1234567890', group=ItemGroup(id='february', title='February'), column_values=(ItemColumnValue(id='status', type='status', text='Working on it'), ItemColumnValue(id='date4', type='date', text='2026-02-18')))))

setattr(update_item_column_values_scheduled_work_item, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_not_found_failure() -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(NOT_FOUND, UpdateItemColumnValuesOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateItemColumnValues", "The board or item does not exist or the connection cannot see it; nothing was changed."))

setattr(update_item_column_values_not_found_failure, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_provider_rejected_failure() -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateItemColumnValuesOutput(), Failure(FailureKind.VALIDATION, CONNECTOR_ID, "updateItemColumnValues", "monday.com rejected the status label."))

setattr(update_item_column_values_provider_rejected_failure, '__module__', "dex_connectors.monday.mondaymock")

def update_item_column_values_invalid_response_failure() -> Case[UpdateItemColumnValuesOutput]:
    return Case.branch_result(INVALID_RESPONSE, UpdateItemColumnValuesOutput(), Failure(FailureKind.PROTOCOL, CONNECTOR_ID, "updateItemColumnValues", "monday.com returned a malformed item, so the values may have been set; repeating the Step is safe."))

setattr(update_item_column_values_invalid_response_failure, '__module__', "dex_connectors.monday.mondaymock")

def add_update_added(value: AddUpdateOutput, failure: Failure | None = None) -> Case[AddUpdateOutput]:
    return Case.branch_result(ADDED, value, failure)

setattr(add_update_added, '__module__', "dex_connectors.monday.mondaymock")

def add_update_not_found(value: AddUpdateOutput, failure: Failure | None = None) -> Case[AddUpdateOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(add_update_not_found, '__module__', "dex_connectors.monday.mondaymock")

def add_update_provider_rejected(value: AddUpdateOutput, failure: Failure | None = None) -> Case[AddUpdateOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(add_update_provider_rejected, '__module__', "dex_connectors.monday.mondaymock")

def add_update_uncertain(value: AddUpdateOutput, failure: Failure) -> Case[AddUpdateOutput]:
    return Case.uncertain(value, failure)

setattr(add_update_uncertain, '__module__', "dex_connectors.monday.mondaymock")

def add_update_defect(value: AddUpdateOutput, failure: Failure | None = None) -> Case[AddUpdateOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(add_update_defect, '__module__', "dex_connectors.monday.mondaymock")

def add_update_retry(failure: Failure, after_seconds: float = 0) -> Case[AddUpdateOutput]:
    return Case.retry(failure, after_seconds)

setattr(add_update_retry, '__module__', "dex_connectors.monday.mondaymock")

def add_update_work_request_update() -> Case[AddUpdateOutput]:
    return Case.branch_result(ADDED, AddUpdateOutput(item_id='5550004', update_id='3300001', creator_id='48202303', created_at=datetime.fromisoformat("2026-02-18T10:06:00Z".replace('Z', '+00:00'))))

setattr(add_update_work_request_update, '__module__', "dex_connectors.monday.mondaymock")

def add_update_not_found_failure() -> Case[AddUpdateOutput]:
    return Case.branch_result(NOT_FOUND, AddUpdateOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "addUpdate", "The item does not exist or the connection cannot see it; nothing was added."))

setattr(add_update_not_found_failure, '__module__', "dex_connectors.monday.mondaymock")

def add_update_provider_rejected_failure() -> Case[AddUpdateOutput]:
    return Case.branch_result(PROVIDER_REJECTED, AddUpdateOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "addUpdate", "monday.com denied update permission."))

setattr(add_update_provider_rejected_failure, '__module__', "dex_connectors.monday.mondaymock")


class Mock:

    def __init__(self, connection_name: str = "monday") -> None:
        self.list_items_script: Query[ListItemsInput, ListItemsOutput] = Query(LIST_ITEMS_DEFINITION)
        self.list_items_script.default(list_items_completed_work_request_pages())
        self.get_item_script: Query[GetItemInput, Item] = Query(GET_ITEM_DEFINITION)
        self.get_item_script.default(get_item_created_work_item())
        self.create_item_script: Mutation[CreateItemInput, CreateItemOutput] = Mutation(CREATE_ITEM_DEFINITION)
        self.create_item_script.default(create_item_february_work_item())
        self.update_item_column_values_script: Mutation[UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput] = Mutation(UPDATE_ITEM_COLUMN_VALUES_DEFINITION)
        self.update_item_column_values_script.default(update_item_column_values_scheduled_work_item())
        self.add_update_script: Mutation[AddUpdateInput, AddUpdateOutput] = Mutation(ADD_UPDATE_DEFINITION)
        self.add_update_script.default(add_update_work_request_update())
        self._connection = new_connection_with_operations(
            list_items=self.list_items_script,
            get_item=self.get_item_script,
            create_item=self.create_item_script,
            update_item_column_values=self.update_item_column_values_script,
            add_update=self.add_update_script,
            reference=ConnectionRef(CONNECTOR_ID, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.monday.mondaymock")

__all__ = [
    "Mock",
    "list_items_listed",
    "list_items_not_found",
    "list_items_provider_rejected",
    "list_items_invalid_response",
    "list_items_defect",
    "list_items_retry",
    "list_items_completed_work_request_pages",
    "list_items_not_found_failure",
    "list_items_provider_rejected_failure",
    "list_items_invalid_response_failure",
    "get_item_found",
    "get_item_not_found",
    "get_item_provider_rejected",
    "get_item_invalid_response",
    "get_item_defect",
    "get_item_retry",
    "get_item_created_work_item",
    "get_item_not_found_failure",
    "get_item_provider_rejected_failure",
    "get_item_invalid_response_failure",
    "create_item_created",
    "create_item_not_found",
    "create_item_provider_rejected",
    "create_item_uncertain",
    "create_item_defect",
    "create_item_retry",
    "create_item_february_work_item",
    "create_item_not_found_failure",
    "create_item_provider_rejected_failure",
    "update_item_column_values_updated",
    "update_item_column_values_not_found",
    "update_item_column_values_provider_rejected",
    "update_item_column_values_invalid_response",
    "update_item_column_values_defect",
    "update_item_column_values_retry",
    "update_item_column_values_scheduled_work_item",
    "update_item_column_values_not_found_failure",
    "update_item_column_values_provider_rejected_failure",
    "update_item_column_values_invalid_response_failure",
    "add_update_added",
    "add_update_not_found",
    "add_update_provider_rejected",
    "add_update_uncertain",
    "add_update_defect",
    "add_update_retry",
    "add_update_work_request_update",
    "add_update_not_found_failure",
    "add_update_provider_rejected_failure",
]
