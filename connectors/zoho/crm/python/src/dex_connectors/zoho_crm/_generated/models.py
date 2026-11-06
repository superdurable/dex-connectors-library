# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from datetime import UTC, datetime
from typing import Any

from dex_connectors.sdk import format_rfc3339_nano

from .timestamps import canonical_go_timestamp

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# COQLValue is one typed value of a condition. Build it with COQLText, COQLTextList, COQLNumber,
# COQLBoolean, COQLDate, COQLDateTime, COQLRecordID, or COQLRecordIDList, so a value can never
# change the query around it.
@dataclass(frozen=True, slots=True)
class COQLValue:
    # Kind selects the rendering.
    kind: str
    # Text holds the value for every kind except the lists and boolean.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Texts holds the entries of a list.
    texts: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Boolean holds the value of a boolean.
    boolean: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(COQLValue, '__module__', 'dex_connectors.zoho_crm.models')


# RecordCondition is one COQL criterion: a field path, a comparator, and a typed value.
@dataclass(frozen=True, slots=True)
class RecordCondition:
    # Field is a field API name, such as Email, or a lookup path with at most two joins, such as Account_Name.Account_Name.
    field: str = ''
    # Operator is the comparator.
    operator: str = _dataclass_field(kw_only=True)
    # Value is the typed value; it is nil for is null and is not null, and a list for in and not in.
    value: COQLValue | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})


setattr(RecordCondition, '__module__', 'dex_connectors.zoho_crm.models')


# RecordSort orders results by one field; record ID breaks ties in ascending order.
@dataclass(frozen=True, slots=True)
class RecordSort:
    # Field is the field API name to order by, such as Modified_Time.
    field: str = ''
    # IsDescending orders from the highest value instead of the lowest.
    is_descending: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDescending', 'dex_json_omit_default': True})


setattr(RecordSort, '__module__', 'dex_connectors.zoho_crm.models')


# FindRecordsInput finds records of one module whose fields match every condition.
@dataclass(frozen=True, slots=True)
class FindRecordsInput:
    # Module is the module API name, such as Contacts.
    module: str = ''
    # Fields are 1 to 50 field API names or lookup paths to return, such as Email and Account_Name;
    # the record ID is always returned.
    fields: tuple[str, ...] = ()
    # Conditions are 1 to 10 criteria that every returned record matches, such as Email = jane@acme.example.com.
    conditions: tuple[RecordCondition, ...] = ()
    # Sort orders the page by one field and then by record ID; nil orders by record ID.
    sort: RecordSort | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Limit is the page size, 1 to 200; zero uses DefaultFindRecordsLimit.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Offset skips that many matches; pass the previous page's NextOffset to read the next page.
    offset: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'fields', _replace_nil_slices(self.fields, 1))
        object.__setattr__(self, 'conditions', _replace_nil_slices(self.conditions, 1))


FindRecordsInput.__annotations__['fields'] = tuple[str, ...] | None
FindRecordsInput.__dataclass_fields__['fields'].type = tuple[str, ...] | None
FindRecordsInput.__annotations__['conditions'] = tuple[RecordCondition, ...] | None
FindRecordsInput.__dataclass_fields__['conditions'].type = tuple[RecordCondition, ...] | None
setattr(FindRecordsInput, '__module__', 'dex_connectors.zoho_crm.models')


# Record is one Zoho CRM record. Fields holds each returned field's exact JSON value by API name:
# text, numbers, booleans, null, lists, and lookups such as {"name": "Zylker", "id": "4150868000000376008"}.
# Zoho's own keys that start with $, such as $approval, are dropped.
@dataclass(frozen=True, slots=True)
class Record:
    # Module is the module API name, such as Contacts.
    module: str = ''
    # ID is the record ID, a decimal string such as 4150868000000376008.
    id: str = ''
    # Fields holds every returned field except id.
    fields: dict[str, Any] | None = None


setattr(Record, '__module__', 'dex_connectors.zoho_crm.models')


# FindRecordsOutput is one page of matching records.
@dataclass(frozen=True, slots=True)
class FindRecordsOutput:
    # Module is the module API name that was queried.
    module: str = ''
    # Records are the page's matches in the requested order.
    records: tuple[Record, ...] = ()
    # NextOffset is the Offset of the next page, or zero when this page is the last one Zoho CRM can serve.
    next_offset: int = _dataclass_field(default=0, metadata={'dex_json_name': 'nextOffset', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


FindRecordsOutput.__annotations__['records'] = tuple[Record, ...] | None
FindRecordsOutput.__dataclass_fields__['records'].type = tuple[Record, ...] | None
setattr(FindRecordsOutput, '__module__', 'dex_connectors.zoho_crm.models')


# GetRecordInput reads one record by ID.
@dataclass(frozen=True, slots=True)
class GetRecordInput:
    # Module is the module API name, such as Deals.
    module: str = ''
    # RecordID is the record ID, such as 4150868000000376008.
    record_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'recordId'})
    # Fields are up to 50 field API names to return; empty returns every field of the record.
    fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(GetRecordInput, '__module__', 'dex_connectors.zoho_crm.models')


# ListModifiedRecordsInput lists records of one module changed at or after a point, oldest change
# first. Set ModifiedSince for the first page of a new feed, or Cursor to continue one.
@dataclass(frozen=True, slots=True)
class ListModifiedRecordsInput:
    # Module is the module API name, such as Deals.
    module: str = ''
    # Fields are 1 to 49 field API names to return; Modified_Time is always added.
    fields: tuple[str, ...] = ()
    # ModifiedSince starts a feed at records modified at or after this instant, in whole seconds.
    # Leave it zero when Cursor is set.
    modified_since: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'modifiedSince', 'dex_json_omit_default': True})
    # Cursor continues a feed after the last record of a previous page. Leave it empty when ModifiedSince is set.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Limit is the page size, 1 to 200; zero uses DefaultListModifiedRecordsLimit.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.modified_since is not None:
            timestamp = self.modified_since
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'modified_since'))
            object.__setattr__(self, 'modified_since', canonical)
        object.__setattr__(self, 'fields', _replace_nil_slices(self.fields, 1))


ListModifiedRecordsInput.__annotations__['fields'] = tuple[str, ...] | None
ListModifiedRecordsInput.__dataclass_fields__['fields'].type = tuple[str, ...] | None
setattr(ListModifiedRecordsInput, '__module__', 'dex_connectors.zoho_crm.models')


# ListModifiedRecordsOutput is one page of changed records and the watermark after them.
@dataclass(frozen=True, slots=True)
class ListModifiedRecordsOutput:
    # Module is the module API name that was listed.
    module: str = ''
    # Records are ordered by Modified_Time and then record ID.
    records: tuple[Record, ...] = ()
    # HasMore reports that Zoho CRM holds further changed records now.
    has_more: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'hasMore', 'dex_json_omit_default': True})
    # Cursor is the watermark after the last record, or the input's starting point for an empty page.
    # Pass it back to read the next page, or store it to read the next changes in a later poll.
    cursor: str = ''

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


ListModifiedRecordsOutput.__annotations__['records'] = tuple[Record, ...] | None
ListModifiedRecordsOutput.__dataclass_fields__['records'].type = tuple[Record, ...] | None
setattr(ListModifiedRecordsOutput, '__module__', 'dex_connectors.zoho_crm.models')


# UpsertRecordInput creates one record, or updates the record whose duplicate-check field already
# holds the value Fields gives it.
@dataclass(frozen=True, slots=True)
class UpsertRecordInput:
    # Module is the module API name, such as Contacts.
    module: str = ''
    # DuplicateCheckFields are 1 to 3 field API names Zoho CRM checks in order, such as Email for
    # Contacts, Account_Name for Accounts, or a custom field marked Do not allow duplicate values.
    # Each must be set in Fields to a non-empty value, so a repeated attempt finds the record.
    duplicate_check_fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'duplicateCheckFields'})
    # Fields are 1 to 200 field values by API name, such as {"Email": "\"jane@acme.example.com\""};
    # lookups are written as {"id": "<record ID>"} with LookupFieldValue. The record ID cannot be set.
    fields: dict[str, Any] | None = None
    # Triggers limits the automations Zoho CRM runs after the write; empty keeps Zoho CRM's default,
    # which runs workflow rules, approvals, and blueprints.
    triggers: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ShouldSkipAutomation runs no workflow rule, approval, or blueprint after the write.
    should_skip_automation: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldSkipAutomation', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'duplicate_check_fields', _replace_nil_slices(self.duplicate_check_fields, 1))


UpsertRecordInput.__annotations__['duplicate_check_fields'] = tuple[str, ...] | None
UpsertRecordInput.__dataclass_fields__['duplicate_check_fields'].type = tuple[str, ...] | None
setattr(UpsertRecordInput, '__module__', 'dex_connectors.zoho_crm.models')


# ProviderError is one machine-readable Zoho CRM error about a record: its code and, when Zoho
# names one, the field API name and the record that already holds a unique value. It never
# carries Zoho CRM's message text, which can repeat record values.
@dataclass(frozen=True, slots=True)
class ProviderError:
    # Code is Zoho CRM's error code, such as MANDATORY_NOT_FOUND, INVALID_DATA, or DUPLICATE_DATA.
    code: str = ''
    # Field is the API name of the field Zoho CRM named, such as Last_Name, or empty.
    field: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DuplicateRecordID is the record that already holds the value of a unique field, for DUPLICATE_DATA.
    duplicate_record_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'duplicateRecordId', 'dex_json_omit_default': True})


setattr(ProviderError, '__module__', 'dex_connectors.zoho_crm.models')


# UpsertRecordOutput is the record Zoho CRM created or updated.
@dataclass(frozen=True, slots=True)
class UpsertRecordOutput:
    # Module is the module API name.
    module: str = ''
    # ID is the record ID.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsCreated reports that the answered request inserted the record. After a retry it can be
    # false even though an earlier attempt of the same Step created the record.
    is_created: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCreated', 'dex_json_omit_default': True})
    # DuplicateField is the duplicate-check field that matched an existing record, or empty after an insert.
    duplicate_field: str = _dataclass_field(default='', metadata={'dex_json_name': 'duplicateField', 'dex_json_omit_default': True})
    # CreatedAt is the record's creation time.
    created_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True})
    # ModifiedAt is the record's modification time after the write.
    modified_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'modifiedAt', 'dex_json_omit_default': True})
    # ProviderErrors holds Zoho CRM's codes and field names on conflict and recordRejected.
    provider_errors: tuple[ProviderError, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'providerErrors', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.created_at is not None:
            timestamp = self.created_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'created_at'))
            object.__setattr__(self, 'created_at', canonical)
        if self.modified_at is not None:
            timestamp = self.modified_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'modified_at'))
            object.__setattr__(self, 'modified_at', canonical)


setattr(UpsertRecordOutput, '__module__', 'dex_connectors.zoho_crm.models')


# UpdateRecordInput sets named fields of one record; every other field keeps its value.
@dataclass(frozen=True, slots=True)
class UpdateRecordInput:
    # Module is the module API name, such as Deals.
    module: str = ''
    # RecordID is the record ID, such as 4150868000000376008.
    record_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'recordId'})
    # Fields are 1 to 200 field values by API name, such as Stage and Owner; JSON null clears a
    # field, and lookups are written as {"id": "<record ID>"} with LookupFieldValue.
    fields: dict[str, Any] | None = None
    # Triggers limits the automations Zoho CRM runs after the write; empty keeps Zoho CRM's default,
    # which runs workflow rules, approvals, and blueprints.
    triggers: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ShouldSkipAutomation runs no workflow rule, approval, or blueprint after the write.
    should_skip_automation: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'shouldSkipAutomation', 'dex_json_omit_default': True})


setattr(UpdateRecordInput, '__module__', 'dex_connectors.zoho_crm.models')


# UpdateRecordOutput is the result of the update.
@dataclass(frozen=True, slots=True)
class UpdateRecordOutput:
    # Module is the module API name.
    module: str = ''
    # ID is the updated record's ID.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ModifiedAt is the record's modification time after the write.
    modified_at: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'modifiedAt', 'dex_json_omit_default': True})
    # ProviderErrors holds Zoho CRM's codes and field names on conflict and recordRejected.
    provider_errors: tuple[ProviderError, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'providerErrors', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.modified_at is not None:
            timestamp = self.modified_at
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'modified_at'))
            object.__setattr__(self, 'modified_at', canonical)


setattr(UpdateRecordOutput, '__module__', 'dex_connectors.zoho_crm.models')


# ListModuleFieldsInput names the module whose fields to list.
@dataclass(frozen=True, slots=True)
class ListModuleFieldsInput:
    # Module is the module API name, such as Deals.
    module: str = ''


setattr(ListModuleFieldsInput, '__module__', 'dex_connectors.zoho_crm.models')


# PicklistValue is one option of a picklist or multi-select picklist.
@dataclass(frozen=True, slots=True)
class PicklistValue:
    # ActualValue is the value records store and writes send, such as Closed Won.
    actual_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'actualValue'})
    # DisplayValue is the label the Zoho CRM interface shows.
    display_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayValue', 'dex_json_omit_default': True})
    # IsUnused reports an option Zoho CRM keeps but no longer offers.
    is_unused: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isUnused', 'dex_json_omit_default': True})


setattr(PicklistValue, '__module__', 'dex_connectors.zoho_crm.models')


# ModuleField is one field of a module. Values are Zoho CRM's own, such as data type picklist and
# the stage names a deal's Stage accepts.
@dataclass(frozen=True, slots=True)
class ModuleField:
    # APIName is the name every operation uses, such as Stage or a custom field's External_Deal_ID.
    api_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'apiName'})
    # DisplayLabel is the label in the Zoho CRM interface, untranslated.
    display_label: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayLabel', 'dex_json_omit_default': True})
    # DataType is Zoho CRM's field type, such as text, email, picklist, lookup, ownerlookup, currency, or datetime.
    data_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'dataType'})
    # IsCustom reports a field the organization added.
    is_custom: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCustom', 'dex_json_omit_default': True})
    # IsReadOnly reports a field the connection's user cannot change.
    is_read_only: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isReadOnly', 'dex_json_omit_default': True})
    # IsSystemMandatory reports a field every new record needs, such as Last_Name for Contacts.
    is_system_mandatory: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSystemMandatory', 'dex_json_omit_default': True})
    # IsUnique reports a field marked Do not allow duplicate values, which a concurrent upsert relies on.
    is_unique: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isUnique', 'dex_json_omit_default': True})
    # Length is the maximum length of a text value, or zero when Zoho CRM states none.
    length: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # LookupModule is the module a lookup field points at, such as Accounts, or empty.
    lookup_module: str = _dataclass_field(default='', metadata={'dex_json_name': 'lookupModule', 'dex_json_omit_default': True})
    # PicklistValues are a picklist's options in Zoho CRM's order, used and unused.
    picklist_values: tuple[PicklistValue, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'picklistValues', 'dex_json_omit_default': True})


setattr(ModuleField, '__module__', 'dex_connectors.zoho_crm.models')


# ListModuleFieldsOutput is a module's fields as Zoho CRM describes them, across every layout.
@dataclass(frozen=True, slots=True)
class ListModuleFieldsOutput:
    # Module is the module API name.
    module: str = ''
    # Fields are the module's fields in Zoho CRM's order.
    fields: tuple[ModuleField, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'fields', _replace_nil_slices(self.fields, 1))


ListModuleFieldsOutput.__annotations__['fields'] = tuple[ModuleField, ...] | None
ListModuleFieldsOutput.__dataclass_fields__['fields'].type = tuple[ModuleField, ...] | None
setattr(ListModuleFieldsOutput, '__module__', 'dex_connectors.zoho_crm.models')
