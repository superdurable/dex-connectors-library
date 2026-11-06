# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from .timestamps import parse_go_datetime, validate_go_datetime

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ItemFilterRule is one condition on a column's value.
#
# Values carry string compare values in monday.com's per-column format, such as
# an exact item name for the name column, ["EXACT", "2026-07-01"] or ["TODAY"]
# for a date column, or "person-48202303" for a people column. Numbers carry
# numeric compare values, such as status label indexes. Set at most one of the
# two: ItemFilterIsEmpty and ItemFilterIsNotEmpty take neither; contains_text,
# not_contains_text, contains_terms, starts_with, and ends_with take exactly one
# string; between takes exactly two; the other operators take 1 to 50. An
# unsupported combination selects defect before any request is sent.
@dataclass(frozen=True, slots=True)
class ItemFilterRule:
    # ColumnID is the column to compare, such as status or date4; name compares the item name.
    column_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'columnId'})
    # Operator is the comparison; blank means any_of.
    operator: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Values are string compare values.
    values: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Numbers are numeric compare values, such as status label indexes.
    numbers: tuple[float, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CompareAttribute optionally names the column attribute monday.com compares, such as a people column's attribute.
    compare_attribute: str = _dataclass_field(default='', metadata={'dex_json_name': 'compareAttribute', 'dex_json_omit_default': True})


setattr(ItemFilterRule, '__module__', 'dex_connectors.monday.models')


# ItemFilter is a typed items_page filter: rules on column values joined by Combination.
# The zero value matches every active item on the board.
@dataclass(frozen=True, slots=True)
class ItemFilter:
    # Rules are at most 10 column conditions.
    rules: tuple[ItemFilterRule, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Combination joins the rules; blank means every rule must match.
    combination: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(ItemFilter, '__module__', 'dex_connectors.monday.models')


# ItemOrder orders a listing by one column or timestamp.
@dataclass(frozen=True, slots=True)
class ItemOrder:
    # ColumnID is the column to order by, or ItemOrderCreationTime or ItemOrderLastUpdated.
    column_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'columnId'})
    # IsDescending orders from the largest or latest value.
    descending: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(ItemOrder, '__module__', 'dex_connectors.monday.models')


# ListItemsInput selects one page of a board's active items. The first page sets BoardID with
# an optional Filter and Order; a later page sets BoardID and the previous page's NextCursor
# only, because monday.com's cursor carries the filter and rejects a new one.
@dataclass(frozen=True, slots=True)
class ListItemsInput:
    # BoardID is the numeric board ID, the number after /boards/ in the board's URL.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # Filter limits the page to items whose column values match; the zero value lists every active item.
    filter: ItemFilter | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Order orders the listing by one column or timestamp; nil keeps monday.com's board order.
    order: ItemOrder | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Limit is the page size from 1 to 100; zero means 25.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Cursor continues a listing with the previous page's NextCursor; monday.com expires it 60 minutes after the first page.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ColumnIDs limits each item's column values to at most 50 columns; empty returns every column.
    column_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'columnIds', 'dex_json_omit_default': True})


setattr(ListItemsInput, '__module__', 'dex_connectors.monday.models')


# ItemGroup identifies a board group, monday.com's container for items.
@dataclass(frozen=True, slots=True)
class ItemGroup:
    # ID is the group ID, such as topics or new_group29179.
    id: str = ''
    # Title is the group's display title.
    title: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ItemGroup, '__module__', 'dex_connectors.monday.models')


# ItemColumnValue is one column value read from an item.
@dataclass(frozen=True, slots=True)
class ItemColumnValue:
    # ID is the column ID, such as status or date4.
    id: str = ''
    # Type is monday.com's column type, such as status, date, people, or numbers.
    type: str = _dataclass_field(kw_only=True)
    # Text is monday.com's display text, such as Working on it for a status; blank when the column is empty.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Value is the column's raw JSON value, such as {"index":1}; nil when the column is empty or omitted.
    value: Any = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsTextTruncated reports that Text stopped at MaxColumnTextBytes.
    text_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'textTruncated', 'dex_json_omit_default': True})
    # IsValueOmitted reports that the raw value exceeded MaxColumnValueBytes and was left out.
    value_omitted: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'valueOmitted', 'dex_json_omit_default': True})


setattr(ItemColumnValue, '__module__', 'dex_connectors.monday.models')


# Item is the connector's view of one monday.com item. Column types, status labels, and
# other column text are the board's own values, never mapped to another vocabulary.
@dataclass(frozen=True, slots=True)
class Item:
    # ID is monday.com's numeric item ID, as a decimal string.
    id: str = ''
    # Name is the item name, the board's first column.
    name: str = ''
    # State is active, archived, or deleted; blank when monday.com omits it.
    state: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # BoardID is the numeric ID of the board that holds the item.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId', 'dex_json_omit_default': True})
    # Group is the board group that holds the item, or nil when monday.com omits it.
    group: ItemGroup | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # CreatorID is the numeric ID of the user who created the item; blank when monday.com created it.
    creator_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'creatorId', 'dex_json_omit_default': True})
    # URL is the item's page on the account's monday.com host.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CreatedAt is when the item was created, or zero when monday.com's timestamp is absent or unreadable.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when the item last changed, or zero when monday.com's timestamp is absent or unreadable.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # ColumnValues lists the requested columns in monday.com's board order; the name column is Name instead.
    column_values: tuple[ItemColumnValue, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'columnValues', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')


setattr(Item, '__module__', 'dex_connectors.monday.models')


# ListItemsOutput is one page of items.
@dataclass(frozen=True, slots=True)
class ListItemsOutput:
    # BoardID echoes the requested board.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # Items are the page's active items in monday.com's order.
    items: tuple[Item, ...] = ()
    # NextCursor continues the listing; empty means this is the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


ListItemsOutput.__annotations__['items'] = tuple[Item, ...] | None
ListItemsOutput.__dataclass_fields__['items'].type = tuple[Item, ...] | None
setattr(ListItemsOutput, '__module__', 'dex_connectors.monday.models')


# GetItemInput identifies one item.
@dataclass(frozen=True, slots=True)
class GetItemInput:
    # ItemID is the numeric item ID, such as the ID listItems returns.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # ColumnIDs limits the column values to at most 50 columns; empty returns every column.
    column_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'columnIds', 'dex_json_omit_default': True})
    # IncludesInactive also returns an archived or deleted item, with its State, instead of notFound.
    includes_inactive: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'includesInactive', 'dex_json_omit_default': True})


setattr(GetItemInput, '__module__', 'dex_connectors.monday.models')


# ColumnValue is one typed value written to a monday.com column.
#
# Type selects which fields apply: Text for text and long_text; Number for
# numbers; StatusLabel or StatusIndex for status; Date and the optional Time for
# date; PersonIDs and TeamIDs for people; Checked for checkbox; Email with the
# optional Text for email; URL with the optional Text for link; Phone and
# CountryCode for phone; DropdownLabels or DropdownLabelIDs for dropdown; and
# Date and EndDate for timeline. Setting a field that does not belong to Type
# selects defect before any request is sent. Prefer the constructors such as
# TextValue and StatusLabelValue, and ClearedValue to empty a column on purpose.
#
# Writes replace the column's whole value: a people or dropdown value lists
# every entry the column keeps, so repeating the same write leaves the item
# unchanged.
@dataclass(frozen=True, slots=True)
class ColumnValue:
    # Type is the column's monday.com type.
    type: str
    # IsCleared empties the column; no other field may be set.
    cleared: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})
    # Text is the text of a text or long_text value, or the display text of an email or link.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Number is a finite numbers value.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # StatusLabel selects a status label by its text, such as Done.
    status_label: str = _dataclass_field(default='', metadata={'dex_json_name': 'statusLabel', 'dex_json_omit_default': True})
    # StatusIndex selects a status label by its index; monday.com recommends it for numeric labels.
    status_index: int | None = _dataclass_field(default=None, metadata={'dex_json_name': 'statusIndex', 'dex_json_omit_default': True})
    # Date is a YYYY-MM-DD date, or the start of a timeline.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Time is an optional HH:MM:SS UTC time for a date value.
    time: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # EndDate is the YYYY-MM-DD end of a timeline, on or after Date.
    end_date: str = _dataclass_field(default='', metadata={'dex_json_name': 'endDate', 'dex_json_omit_default': True})
    # PersonIDs lists monday.com user IDs of a people value.
    person_ids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'personIds', 'dex_json_omit_default': True})
    # TeamIDs lists monday.com team IDs of a people value.
    team_ids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'teamIds', 'dex_json_omit_default': True})
    # Checked is a checkbox value; false clears the checkbox.
    checked: bool | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Email is a bare email address such as jane@example.com.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # URL is an absolute http or https link.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Phone is a phone number of 4 to 20 digits with an optional leading +.
    phone: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # CountryCode is the phone number's ISO 3166-1 alpha-2 country code, such as US.
    country_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'countryCode', 'dex_json_omit_default': True})
    # DropdownLabels selects dropdown labels by their text.
    dropdown_labels: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'dropdownLabels', 'dex_json_omit_default': True})
    # DropdownLabelIDs selects dropdown labels by their numeric IDs.
    dropdown_label_ids: tuple[int, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'dropdownLabelIds', 'dex_json_omit_default': True})


setattr(ColumnValue, '__module__', 'dex_connectors.monday.models')


# CreateItemInput describes one new item.
@dataclass(frozen=True, slots=True)
class CreateItemInput:
    # BoardID is the numeric board ID, the number after /boards/ in the board's URL.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # GroupID places the item in this group, such as topics; blank uses the board's first active group.
    group_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupId', 'dex_json_omit_default': True})
    # ItemName is the item name, 1 to 255 characters on one line.
    item_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemName'})
    # ColumnValues maps at most 50 column IDs, such as status or date4, to typed values.
    column_values: dict[str, ColumnValue] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'columnValues', 'dex_json_omit_default': True})
    # CreatesLabelsIfMissing lets monday.com add a missing status or dropdown label, which needs
    # permission to change the board structure; false rejects an unknown label.
    creates_labels_if_missing: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'createsLabelsIfMissing', 'dex_json_omit_default': True})


setattr(CreateItemInput, '__module__', 'dex_connectors.monday.models')


# CreateItemOutput identifies the created item. On every other branch ItemID is empty and the
# requested board, group, and name are echoed so the application can reconcile.
@dataclass(frozen=True, slots=True)
class CreateItemOutput:
    # BoardID echoes the requested board.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # GroupID echoes the requested group; blank when none was requested.
    group_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'groupId', 'dex_json_omit_default': True})
    # ItemName echoes the requested name.
    item_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemName'})
    # ItemID is the created item's numeric ID.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId', 'dex_json_omit_default': True})
    # Item is the created item without column values, which keeps the response replayable.
    item: Item | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsReplayed reports that monday.com answered an earlier attempt's cached result for this Step's idempotency key.
    replayed: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(CreateItemOutput, '__module__', 'dex_connectors.monday.models')


# UpdateItemColumnValuesInput sets absolute values on columns of one item. Columns left out keep their values.
@dataclass(frozen=True, slots=True)
class UpdateItemColumnValuesInput:
    # BoardID is the numeric ID of the board that holds the item.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # ItemID is the numeric item ID.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # ColumnValues maps 1 to 50 column IDs, such as status or date4, to typed values.
    column_values: dict[str, ColumnValue] | None = _dataclass_field(default=None, metadata={'dex_json_name': 'columnValues'})
    # CreatesLabelsIfMissing lets monday.com add a missing status or dropdown label, which needs
    # permission to change the board structure; false rejects an unknown label.
    creates_labels_if_missing: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'createsLabelsIfMissing', 'dex_json_omit_default': True})


setattr(UpdateItemColumnValuesInput, '__module__', 'dex_connectors.monday.models')


# UpdateItemColumnValuesOutput is the item after the change. On every other branch Item is nil and
# the requested board and item are echoed.
@dataclass(frozen=True, slots=True)
class UpdateItemColumnValuesOutput:
    # BoardID echoes the requested board.
    board_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'boardId'})
    # ItemID echoes the requested item.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # Item is the changed item with the values of the columns this Step set.
    item: Item | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsReplayed reports that monday.com answered an earlier attempt's cached result for this Step's idempotency key.
    replayed: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(UpdateItemColumnValuesOutput, '__module__', 'dex_connectors.monday.models')


# AddUpdateInput is one update, monday.com's comment on an item.
@dataclass(frozen=True, slots=True)
class AddUpdateInput:
    # ItemID is the numeric ID of the item to comment on.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # Body is plain text of at most 10,000 characters. The connector escapes it into monday.com's
    # HTML update body, so markup is shown literally and line breaks are kept.
    body: str = ''


setattr(AddUpdateInput, '__module__', 'dex_connectors.monday.models')


# AddUpdateOutput identifies the added update. On every other branch UpdateID is empty.
@dataclass(frozen=True, slots=True)
class AddUpdateOutput:
    # ItemID echoes the requested item.
    item_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'itemId'})
    # UpdateID is the new update's numeric ID.
    update_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'updateId', 'dex_json_omit_default': True})
    # CreatorID is the numeric ID of the user the update is posted as.
    creator_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'creatorId', 'dex_json_omit_default': True})
    # CreatedAt is when monday.com recorded the update, or zero when its timestamp is unreadable.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # IsReplayed reports that monday.com answered an earlier attempt's cached result for this Step's idempotency key.
    replayed: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')


setattr(AddUpdateOutput, '__module__', 'dex_connectors.monday.models')
