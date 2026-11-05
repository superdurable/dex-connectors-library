# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# SearchInput selects one page of title matches among the pages and data
# sources shared with the connection.
@dataclass(frozen=True, slots=True)
class SearchInput:
    # Query is matched against titles; blank lists every shared page and data source.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ObjectType limits the results to pages or data sources; blank returns both.
    object_type: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'objectType', 'dex_json_omit_default': True})
    # SortDirection orders by last edit time; blank keeps Notion's relevance order.
    sort_direction: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sortDirection', 'dex_json_omit_default': True})
    # PageSize is the number of results to request, 1 to 100; zero requests 20.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # StartCursor continues a previous search with the same input; blank reads the first page.
    start_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'startCursor', 'dex_json_omit_default': True})


setattr(SearchInput, '__module__', 'dex_connectors.notion.models')


# Parent identifies the object that contains a page or data source.
@dataclass(frozen=True, slots=True)
class Parent:
    # Type is the parent's kind; other kinds Notion adds are kept with an empty ID.
    type: str
    # ID is the parent's ID; it is empty for the workspace.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DatabaseID is the database that contains a data source parent.
    database_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'databaseId', 'dex_json_omit_default': True})


setattr(Parent, '__module__', 'dex_connectors.notion.models')


# SearchMatch is one page or data source whose title matched.
@dataclass(frozen=True, slots=True)
class SearchMatch:
    # ObjectType is page or data_source.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # ID is the page or data source ID.
    id: str = ''
    # Title is the plain-text title.
    title: str = ''
    # URL opens the object in Notion; it is not a stable identifier.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Parent contains the object. A data source's parent is its database.
    parent: Parent = _dataclass_field(kw_only=True)
    # LastEditedTime is the RFC 3339 time of the last edit.
    last_edited_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastEditedTime', 'dex_json_omit_default': True})
    # IsInTrash reports that the object is in the trash.
    in_trash: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'inTrash'})


setattr(SearchMatch, '__module__', 'dex_connectors.notion.models')


# SearchOutput is one page of title matches.
@dataclass(frozen=True, slots=True)
class SearchOutput:
    # Matches lists the page's results in Notion's order.
    matches: tuple[SearchMatch, ...] = ()
    # NextCursor continues the search; it is empty on the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})
    # HasMore reports that another page of results exists.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'matches', _replace_nil_slices(self.matches, 1))


SearchOutput.__annotations__['matches'] = tuple[SearchMatch, ...] | None
SearchOutput.__dataclass_fields__['matches'].type = tuple[SearchMatch, ...] | None
setattr(SearchOutput, '__module__', 'dex_connectors.notion.models')


# QueryFilter is a typed Notion data source filter. Set exactly one of
# Property, Timestamp, And, and Or.
#
# A property condition names the property, its Type, a Condition the type
# supports, and the one value field the condition takes: Text for title,
# rich_text, url, email, phone_number, select, status, and multi_select;
# Number for number and unique_id; Checkbox for checkbox; Date for date; and
# ID for people and relation. FilterIsEmpty, FilterIsNotEmpty, and the
# relative date conditions such as FilterPastWeek take no value. A timestamp
# condition sets Timestamp, a date Condition, and Date. And and Or combine up to
# two levels of nested filters. An unsupported combination selects defect
# before any request is sent.
@dataclass(frozen=True, slots=True)
class QueryFilter:
    # Property is the property name or ID that the condition reads.
    property: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Type is the property's Notion type, which selects the filter's shape.
    type: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Timestamp filters on the page's creation or last edit time instead of a property.
    timestamp: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Condition is the comparison.
    condition: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Text is a text or option-name value.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Number is a number value.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Checkbox is a checkbox value.
    checkbox: bool | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Date is a YYYY-MM-DD date or RFC 3339 date-time value.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ID is a Notion user ID for people or a page ID for relation.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # And matches pages that match every filter.
    and_: tuple["QueryFilter", ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'and', 'dex_json_omit_default': True})
    # Or matches pages that match any filter.
    or_: tuple["QueryFilter", ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'or', 'dex_json_omit_default': True})


setattr(QueryFilter, '__module__', 'dex_connectors.notion.models')


# QuerySort orders query results by a property or a timestamp. Set exactly
# one of Property and Timestamp.
@dataclass(frozen=True, slots=True)
class QuerySort:
    # Property is the property name or ID to sort by.
    property: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Timestamp sorts by the page's creation or last edit time.
    timestamp: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Direction is ascending or descending; blank is ascending.
    direction: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(QuerySort, '__module__', 'dex_connectors.notion.models')


# QueryDatabaseInput selects one page of a database's rows. Set exactly one of
# DataSourceID and DatabaseID.
@dataclass(frozen=True, slots=True)
class QueryDatabaseInput:
    # DataSourceID is the data source to query. In Notion, open the database's
    # settings menu > Manage data sources and choose Copy data source ID.
    data_source_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'dataSourceId', 'dex_json_omit_default': True})
    # DatabaseID is the database ID from its URL, or the URL itself; the database
    # must hold exactly one data source.
    database_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'databaseId', 'dex_json_omit_default': True})
    # Filter optionally limits the rows; nil returns every row.
    filter: QueryFilter | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Sorts optionally orders the rows; Notion guarantees no order without one.
    sorts: tuple[QuerySort, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Properties optionally lists the property names or IDs to return, at most
    # 100; blank returns every property.
    properties: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # PageSize is the number of rows to request, 1 to 100; zero requests 50.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # StartCursor continues a previous query with the same input; blank reads the first page.
    start_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'startCursor', 'dex_json_omit_default': True})


setattr(QueryDatabaseInput, '__module__', 'dex_connectors.notion.models')


# DateRange is a Notion date value: one date or date-time, or a range.
@dataclass(frozen=True, slots=True)
class DateRange:
    # Start is a YYYY-MM-DD date or an RFC 3339 date-time such as 2026-09-30T14:00:00Z.
    start: str = ''
    # End optionally ends a range, in the same format as Start; blank means a single date.
    end: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TimeZone optionally names an IANA time zone, such as America/Los_Angeles, for a
    # date-time without an offset; blank uses the offset in Start.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})


setattr(DateRange, '__module__', 'dex_connectors.notion.models')


# PageProperty is one property value read from a Notion page. The embedded
# PropertyValue holds the typed value; read-only types fill the closest typed
# field, such as Number for a numeric formula, Date for created_time, and
# UserIDs for created_by.
@dataclass(frozen=True, slots=True)
class PageProperty:
    # ID is Notion's stable property ID.
    id: str = ''
    # Type is the property's Notion type.
    type: str = _dataclass_field(kw_only=True)
    # Text is the plain text of a title or rich_text value, at most 20,000 characters.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Number is a finite number value.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Checkbox is a checkbox value.
    checkbox: bool | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Option is a select or status option name. Notion adds a missing select
    # option to the data source; a status option must already exist.
    option: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Options lists multi_select option names, at most 100, without commas.
    options: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Date is a date value.
    date: DateRange | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # URL is an absolute URL value.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is a plain email address.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PhoneNumber is free-form phone text, such as +1 206 555 0100.
    phone_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'phoneNumber', 'dex_json_omit_default': True})
    # UserIDs lists Notion user IDs for a people value, at most 100.
    user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'userIds', 'dex_json_omit_default': True})
    # PageIDs lists related page IDs for a relation value, at most 100.
    page_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'pageIds', 'dex_json_omit_default': True})
    # PlainText renders the value as one line of text for display and comparison.
    plain_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'plainText'})
    # IsTruncated reports that Notion returned only the first entries of a
    # relation, people, or rich text value; read the property in Notion for the rest.
    truncated: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(PageProperty, '__module__', 'dex_connectors.notion.models')


# Page is one Notion page with its property values. A database row is a page
# whose parent is a data source.
@dataclass(frozen=True, slots=True)
class Page:
    # ID is the page ID, the page's stable identifier.
    id: str = ''
    # URL opens the page in Notion; Notion does not keep it stable, so store ID instead.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Title is the plain text of the page's title property.
    title: str = ''
    # Parent contains the page.
    parent: Parent = _dataclass_field(kw_only=True)
    # CreatedTime is the RFC 3339 creation time.
    created_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdTime', 'dex_json_omit_default': True})
    # LastEditedTime is the RFC 3339 time of the last edit.
    last_edited_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'lastEditedTime', 'dex_json_omit_default': True})
    # IsInTrash reports that the page was moved to the trash.
    in_trash: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'inTrash'})
    # IsArchived reports that the page was archived, which is separate from the trash.
    archived: bool = False
    # Properties maps each property name to its value. Notion omits the
    # properties of a page the connection can see only partially.
    properties: dict[str, PageProperty] | None = None


setattr(Page, '__module__', 'dex_connectors.notion.models')


# QueryDatabaseOutput is one page of rows.
@dataclass(frozen=True, slots=True)
class QueryDatabaseOutput:
    # DataSourceID is the data source that was queried; store it to skip the database lookup.
    data_source_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'dataSourceId'})
    # Pages lists the page's rows in the requested order.
    pages: tuple[Page, ...] = ()
    # NextCursor continues the query; it is empty on the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})
    # HasMore reports that another page of rows exists.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore'})
    # IsResultLimitReached reports that Notion stopped at its 10,000-row limit for one
    # query; narrow the filter, such as by created_time, to read the rest.
    result_limit_reached: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'resultLimitReached'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'pages', _replace_nil_slices(self.pages, 1))


QueryDatabaseOutput.__annotations__['pages'] = tuple[Page, ...] | None
QueryDatabaseOutput.__dataclass_fields__['pages'].type = tuple[Page, ...] | None
setattr(QueryDatabaseOutput, '__module__', 'dex_connectors.notion.models')


# GetPageInput selects one page.
@dataclass(frozen=True, slots=True)
class GetPageInput:
    # PageID is the page ID, with or without dashes, or the page's Notion URL.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # ShouldSkipContent reads only the page's properties, without its blocks.
    skip_content: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'skipContent', 'dex_json_omit_default': True})
    # MaxBlocks bounds the top-level blocks read, 1 to 500; zero reads up to 100.
    max_blocks: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxBlocks', 'dex_json_omit_default': True})
    # MaxTextCharacters bounds Content.Text, 1 to 100,000 characters; zero keeps up to 20,000.
    max_text_characters: int = _dataclass_field(default=0, metadata={'dex_json_name': 'maxTextCharacters', 'dex_json_omit_default': True})


setattr(GetPageInput, '__module__', 'dex_connectors.notion.models')


# PageContent is a bounded plain-text rendering of a page's top-level blocks.
#
# Each block becomes one line: list items start with "- " or a number, to-do
# items with "[ ] " or "[x] ", table rows join their cells with " | ", and child
# pages and databases show their titles. Blocks inside toggles, columns, and
# other parents are not read; NestedBlockCount counts the blocks that have them.
@dataclass(frozen=True, slots=True)
class PageContent:
    # Text is the rendered content, at most MaxTextCharacters characters.
    text: str = ''
    # BlockCount is the number of top-level blocks read.
    block_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'blockCount'})
    # HasMoreBlocks reports that the page has more top-level blocks than MaxBlocks.
    has_more_blocks: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMoreBlocks'})
    # IsTextTruncated reports that Text was cut at MaxTextCharacters.
    text_truncated: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'textTruncated'})
    # NestedBlockCount counts read blocks whose own child blocks were not read.
    nested_block_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nestedBlockCount'})
    # SkippedBlockCount counts read blocks with no text rendering, such as images and dividers.
    skipped_block_count: int = _dataclass_field(default=0, metadata={'dex_json_name': 'skippedBlockCount'})


setattr(PageContent, '__module__', 'dex_connectors.notion.models')


# GetPageOutput is one page with its properties and bounded content.
@dataclass(frozen=True, slots=True)
class GetPageOutput:
    # Page holds the page's metadata and property values.
    page: Page
    # Content renders the page's top-level blocks as plain text; it is empty when ShouldSkipContent is set.
    content: PageContent = _dataclass_field(default_factory=PageContent)


setattr(GetPageOutput, '__module__', 'dex_connectors.notion.models')


# PropertyValue is one typed property value written to a Notion page.
#
# Type selects exactly one value field: Text for title and rich_text, Number,
# Checkbox, Option for select and status, Options for multi_select, Date, URL,
# Email, PhoneNumber, UserIDs for people, and PageIDs for relation. Setting a
# field that does not belong to Type selects defect. The zero value of the
# type's field clears the property, except that a checkbox needs an explicit
# value and a status needs an option name. Prefer the constructors such as
# TitleValue and SelectValue, and ClearedValue to clear a property on purpose.
#
# Writes replace the property's whole value: a multi_select, people, or
# relation value lists every entry the property keeps, so repeating the same
# write leaves the page unchanged.
@dataclass(frozen=True, slots=True)
class PropertyValue:
    # Type is the property's Notion type.
    type: str
    # Text is the plain text of a title or rich_text value, at most 20,000 characters.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Number is a finite number value.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Checkbox is a checkbox value.
    checkbox: bool | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Option is a select or status option name. Notion adds a missing select
    # option to the data source; a status option must already exist.
    option: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Options lists multi_select option names, at most 100, without commas.
    options: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Date is a date value.
    date: DateRange | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # URL is an absolute URL value.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Email is a plain email address.
    email: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PhoneNumber is free-form phone text, such as +1 206 555 0100.
    phone_number: str = _dataclass_field(default='', metadata={'dex_json_name': 'phoneNumber', 'dex_json_omit_default': True})
    # UserIDs lists Notion user IDs for a people value, at most 100.
    user_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'userIds', 'dex_json_omit_default': True})
    # PageIDs lists related page IDs for a relation value, at most 100.
    page_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'pageIds', 'dex_json_omit_default': True})


setattr(PropertyValue, '__module__', 'dex_connectors.notion.models')


# CreatePageInput describes one database row. Set exactly one of DataSourceID and DatabaseID.
@dataclass(frozen=True, slots=True)
class CreatePageInput:
    # DataSourceID is the data source that receives the row. In Notion, open the
    # database's settings menu > Manage data sources and choose Copy data source ID.
    data_source_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'dataSourceId', 'dex_json_omit_default': True})
    # DatabaseID is the database ID from its URL, or the URL itself; the database
    # must hold exactly one data source.
    database_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'databaseId', 'dex_json_omit_default': True})
    # Properties maps property names or IDs to typed values, at most 100. A
    # property left out keeps the data source's default, such as an empty title.
    properties: dict[str, PropertyValue] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # BodyText is optional plain text for the page body, at most 50,000 characters
    # in at most 100 paragraphs. Blank lines separate paragraphs, other line breaks
    # stay inside a paragraph, and the text is never read as markup.
    body_text: str = _dataclass_field(default='', metadata={'dex_json_name': 'bodyText', 'dex_json_omit_default': True})


setattr(CreatePageInput, '__module__', 'dex_connectors.notion.models')


# CreatePageOutput identifies the created row. On every other branch PageID is
# empty and DataSourceID names the data source when it was resolved.
@dataclass(frozen=True, slots=True)
class CreatePageOutput:
    # DataSourceID is the data source that received, or would have received, the row.
    data_source_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'dataSourceId', 'dex_json_omit_default': True})
    # PageID is the created page's ID.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId', 'dex_json_omit_default': True})
    # Page is the created page, or nil on a non-created outcome.
    page: Page | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsConfirmedAfterTimeout reports a 503 with a saved page ID confirmed during
    # this attempt; a later attempt's read-back cannot recover the original status.
    confirmed_after_timeout: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'confirmedAfterTimeout', 'dex_json_omit_default': True})


setattr(CreatePageOutput, '__module__', 'dex_connectors.notion.models')


# UpdatePagePropertiesInput sets property values on one page. Properties left
# out keep their values; every listed value replaces the property's whole
# value, so repeating the same input leaves the page unchanged.
@dataclass(frozen=True, slots=True)
class UpdatePagePropertiesInput:
    # PageID is the page ID, with or without dashes, or the page's Notion URL.
    page_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageId'})
    # Properties maps property names or IDs to typed values, 1 to 100. Use
    # ClearedValue to empty a property.
    properties: dict[str, PropertyValue] | None = None


setattr(UpdatePagePropertiesInput, '__module__', 'dex_connectors.notion.models')


# UpdatePagePropertiesOutput is the page after the update.
@dataclass(frozen=True, slots=True)
class UpdatePagePropertiesOutput:
    # Page is the page Notion returned after applying the values; it is empty on other branches.
    page: Page


setattr(UpdatePagePropertiesOutput, '__module__', 'dex_connectors.notion.models')
