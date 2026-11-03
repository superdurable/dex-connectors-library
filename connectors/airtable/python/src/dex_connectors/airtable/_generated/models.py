# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# FieldEqualityFilter matches records whose field equals one typed value.
# Exactly one of Text, Number, and Checkbox is set; prefer the constructors
# TextFieldEquals, NumberFieldEquals, and CheckboxFieldEquals.
# 
# The connector writes the filter as an Airtable formula such as
# {Policy Key} = "refund-standard", so it follows Airtable's formula
# comparison rules. A linked record, lookup, or multiple select field compares
# its displayed text, such as the linked records' primary field values joined
# by commas.
@dataclass(frozen=True, slots=True)
class FieldEqualityFilter:
    # Field is the field name, or its field ID such as fldXXXXXXXXXXXXXX. A
    # name cannot contain a brace or a backslash, because Airtable documents no
    # escape for them inside a field reference; use the field ID instead.
    field: str = ''
    # Text matches a text, email, URL, phone, single select, or date field.
    # It cannot contain a backslash or a control character such as a line
    # break; use ListRecordsInput.Formula for such values.
    text: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Number matches a number, currency, percent, rating, duration, count, or
    # autonumber field. It must be finite.
    number: float | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Checkbox matches a checked box when true and an unchecked box when false.
    checkbox: bool | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(FieldEqualityFilter, '__module__', 'dex_connectors.airtable.models')


# RecordSort orders listed records by one field.
@dataclass(frozen=True, slots=True)
class RecordSort:
    # Field is the field name or field ID.
    field: str = ''
    # Direction is asc or desc; blank sorts ascending.
    direction: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(RecordSort, '__module__', 'dex_connectors.airtable.models')


# ListRecordsInput selects one page of a table's records.
# 
# Filters and Formula combine with AND into Airtable's filterByFormula: typed
# equality filters are escaped by the connector, and Formula is passed through
# as written, so it should come from application code rather than end users.
# Leaving both blank lists every record. Without Sort or View, Airtable
# returns records in no particular order.
@dataclass(frozen=True, slots=True)
class ListRecordsInput:
    # BaseID is the base ID, such as appXXXXXXXXXXXXXX, from the base picker.
    base_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'baseId'})
    # TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX from the table
    # picker, which survives renames, or the table name.
    table_id_or_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableIdOrName'})
    # Filters match records whose fields equal typed values, at most 25.
    filters: tuple[FieldEqualityFilter, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Formula is an optional raw Airtable formula, such as {Amount} > 100, ANDed with Filters.
    formula: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Fields limits each returned record to these field names or IDs; blank returns every non-empty field.
    fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Sort orders the records and overrides View's order, at most 10 entries.
    sort: tuple[RecordSort, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ViewIDOrName limits the records to one view, in its order unless Sort is set.
    view_id_or_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'viewIdOrName', 'dex_json_omit_default': True})
    # PageSize is the most records returned, 1 to 100; zero returns up to 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # Offset continues a listing from the previous page's Offset; blank starts at the first page.
    offset: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(ListRecordsInput, '__module__', 'dex_connectors.airtable.models')


# Record is one Airtable record as Airtable returned it.
@dataclass(frozen=True, slots=True)
class Record:
    # ID is the record ID, such as recXXXXXXXXXXXXXX.
    id: str = ''
    # CreatedTime is when Airtable created the record, in UTC.
    created_time: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdTime', 'dex_json_format': 'rfc3339'})
    # Fields maps each non-empty field, by field name, to its cell value.
    # Airtable omits empty fields, so a missing key means the field is empty.
    fields: dict[str, Any] | None = None


setattr(Record, '__module__', 'dex_connectors.airtable.models')


# RecordPage is one page of listed records.
@dataclass(frozen=True, slots=True)
class RecordPage:
    # Records holds at most the requested page size of records; an empty page is an empty list.
    records: tuple[Record, ...] = ()
    # Offset continues the listing on the next call; blank means this is the last page.
    offset: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # FilterFormula is the filterByFormula the connector sent, blank when the call had no filter.
    filter_formula: str = _dataclass_field(default='', metadata={'dex_json_name': 'filterFormula', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


RecordPage.__annotations__['records'] = tuple[Record, ...] | None
RecordPage.__dataclass_fields__['records'].type = tuple[Record, ...] | None
setattr(RecordPage, '__module__', 'dex_connectors.airtable.models')


# GetRecordInput identifies one record.
@dataclass(frozen=True, slots=True)
class GetRecordInput:
    # BaseID is the base ID, such as appXXXXXXXXXXXXXX.
    base_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'baseId'})
    # TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX, or the table name.
    table_id_or_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableIdOrName'})
    # RecordID is the record ID, such as recXXXXXXXXXXXXXX.
    record_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'recordId'})


setattr(GetRecordInput, '__module__', 'dex_connectors.airtable.models')


# RecordUpsert is one record written by UpsertRecords.
@dataclass(frozen=True, slots=True)
class RecordUpsert:
    # Fields maps field names or IDs to cell values. It must hold a non-null
    # value for every merge field, under the same name or ID the merge list uses.
    fields: dict[str, Any] | None = None


setattr(RecordUpsert, '__module__', 'dex_connectors.airtable.models')


# UpsertRecordsInput creates or updates up to 10 records in one table.
# 
# Airtable matches each record to an existing record whose merge fields equal
# the record's values for them: no match creates a record, one match updates
# it, leaving fields the write does not name unchanged, and several matches
# reject the whole request. A repeated write of the same input therefore
# updates the records the first write created instead of adding duplicates.
@dataclass(frozen=True, slots=True)
class UpsertRecordsInput:
    # BaseID is the base ID, such as appXXXXXXXXXXXXXX.
    base_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'baseId'})
    # TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX, or the table name.
    table_id_or_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableIdOrName'})
    # FieldsToMergeOn names one to three fields whose values identify a record,
    # such as a case ID. Airtable lists number, text, long text, single
    # select, multiple select, and date fields as merge fields, never a
    # computed field such as a formula, lookup, or rollup.
    fields_to_merge_on: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'fieldsToMergeOn'})
    # Records are the one to ten records to write; no two may share merge values.
    records: tuple[RecordUpsert, ...] = ()
    # Typecast lets Airtable convert text to the field's type, such as adding a
    # new select option; false rejects a value of the wrong type.
    typecast: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'fields_to_merge_on', _replace_nil_slices(self.fields_to_merge_on, 1))
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


UpsertRecordsInput.__annotations__['fields_to_merge_on'] = tuple[str, ...] | None
UpsertRecordsInput.__dataclass_fields__['fields_to_merge_on'].type = tuple[str, ...] | None
UpsertRecordsInput.__annotations__['records'] = tuple[RecordUpsert, ...] | None
UpsertRecordsInput.__dataclass_fields__['records'].type = tuple[RecordUpsert, ...] | None
setattr(UpsertRecordsInput, '__module__', 'dex_connectors.airtable.models')


# UpsertedRecords reports the records an upsert wrote.
@dataclass(frozen=True, slots=True)
class UpsertedRecords:
    # Records are the written records in input order, with every non-empty field.
    records: tuple[Record, ...] = ()
    # CreatedRecordIDs are the records this answered request created. After a
    # retry whose earlier attempt created a record but lost the response, that
    # record appears in UpdatedRecordIDs instead.
    created_record_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'createdRecordIds'})
    # UpdatedRecordIDs are the existing records this answered request updated.
    updated_record_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'updatedRecordIds'})
    # PartialSuccessReasons names why Airtable saved the records without every
    # attachment, such as attachmentsFailedUploading; empty when nothing failed.
    partial_success_reasons: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'partialSuccessReasons', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))
        object.__setattr__(self, 'created_record_ids', _replace_nil_slices(self.created_record_ids, 1))
        object.__setattr__(self, 'updated_record_ids', _replace_nil_slices(self.updated_record_ids, 1))


UpsertedRecords.__annotations__['records'] = tuple[Record, ...] | None
UpsertedRecords.__dataclass_fields__['records'].type = tuple[Record, ...] | None
UpsertedRecords.__annotations__['created_record_ids'] = tuple[str, ...] | None
UpsertedRecords.__dataclass_fields__['created_record_ids'].type = tuple[str, ...] | None
UpsertedRecords.__annotations__['updated_record_ids'] = tuple[str, ...] | None
UpsertedRecords.__dataclass_fields__['updated_record_ids'].type = tuple[str, ...] | None
setattr(UpsertedRecords, '__module__', 'dex_connectors.airtable.models')


# RecordUpdate sets fields on one existing record.
@dataclass(frozen=True, slots=True)
class RecordUpdate:
    # ID is the record ID, such as recXXXXXXXXXXXXXX.
    id: str = ''
    # Fields maps field names or IDs to their new cell values. Fields the
    # update does not name keep their values.
    fields: dict[str, Any] | None = None


setattr(RecordUpdate, '__module__', 'dex_connectors.airtable.models')


# UpdateRecordsInput updates up to 10 records in one table by record ID.
# 
# Each value replaces the field's whole value, including every link of a
# linked record field, so repeating the same update leaves the records
# unchanged.
@dataclass(frozen=True, slots=True)
class UpdateRecordsInput:
    # BaseID is the base ID, such as appXXXXXXXXXXXXXX.
    base_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'baseId'})
    # TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX, or the table name.
    table_id_or_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'tableIdOrName'})
    # Records are the one to ten records to update, each named once.
    records: tuple[RecordUpdate, ...] = ()
    # Typecast lets Airtable convert text to the field's type, such as adding a
    # new select option; false rejects a value of the wrong type.
    typecast: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


UpdateRecordsInput.__annotations__['records'] = tuple[RecordUpdate, ...] | None
UpdateRecordsInput.__dataclass_fields__['records'].type = tuple[RecordUpdate, ...] | None
setattr(UpdateRecordsInput, '__module__', 'dex_connectors.airtable.models')


# UpdatedRecords reports the records an update wrote.
@dataclass(frozen=True, slots=True)
class UpdatedRecords:
    # Records are the updated records with every non-empty field.
    records: tuple[Record, ...] = ()
    # PartialSuccessReasons names why Airtable saved the records without every
    # attachment, such as attachmentsFailedUploading; empty when nothing failed.
    partial_success_reasons: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'partialSuccessReasons', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


UpdatedRecords.__annotations__['records'] = tuple[Record, ...] | None
UpdatedRecords.__dataclass_fields__['records'].type = tuple[Record, ...] | None
setattr(UpdatedRecords, '__module__', 'dex_connectors.airtable.models')
