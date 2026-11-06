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


# SOQLValue is one typed value bound to a :name placeholder in a
# QueryRecordsInput SOQL statement. The connector renders it as an escaped
# literal, so bound text can never change the statement's structure. Build it
# with a constructor such as SOQLString; the exported fields exist so a value
# can travel in durable Step input.
@dataclass(frozen=True, slots=True)
class SOQLValue:
    # Kind selects the rendering.
    kind: str
    # Text holds the value for every kind except stringList, boolean, and null.
    text: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Texts holds the values of a stringList.
    texts: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Boolean holds the value of a boolean.
    boolean: bool = _dataclass_field(default=False, metadata={'dex_json_omit_default': True})


setattr(SOQLValue, '__module__', 'dex_connectors.salesforce.models')


# QueryRecordsInput selects one page of a SOQL query. Set exactly one of SOQL
# and NextRecordsCursor.
#
# SOQL values are never concatenated into the statement: write a :name
# placeholder, as in Apex, and bind a typed value such as SOQLString(email)
# under that name. Placeholders inside string literals are left untouched, and
# every binding must be used exactly by name.
@dataclass(frozen=True, slots=True)
class QueryRecordsInput:
    # SOQL is one SOQL statement with :name placeholders, such as
    # SELECT Id, Name FROM Contact WHERE Email = :email LIMIT 10.
    soql: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Bindings maps each placeholder name, without its colon, to a typed value.
    bindings: dict[str, SOQLValue] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # NextRecordsCursor continues a query with the cursor a previous page returned.
    next_records_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextRecordsCursor', 'dex_json_omit_default': True})
    # BatchSize requests from 200 to 2000 records per page; zero uses the
    # connection's queryBatchSize. Salesforce treats it as a hint.
    batch_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'batchSize', 'dex_json_omit_default': True})


setattr(QueryRecordsInput, '__module__', 'dex_connectors.salesforce.models')


# Record is one Salesforce record. Fields keeps each returned field's exact
# JSON value, so numbers, dates, and nested relationship records are never
# reinterpreted; a relationship value is an object with its own attributes.
@dataclass(frozen=True, slots=True)
class Record:
    # Type is the record's sObject API name, such as Contact, or AggregateResult for aggregate rows.
    type: str = ''
    # ID is the 18-character record ID when the record returned its Id field.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Fields maps each returned field API name, including Id, to its JSON value.
    fields: dict[str, Any] | None = None


setattr(Record, '__module__', 'dex_connectors.salesforce.models')


# QueryRecordsOutput is one page of query results.
@dataclass(frozen=True, slots=True)
class QueryRecordsOutput:
    # Records lists the page's records in Salesforce's order. An aggregate
    # query such as SELECT COUNT() FROM Contact returns no records and reports
    # its count in TotalSize.
    records: tuple[Record, ...] = ()
    # TotalSize is the number of rows the whole query matches, across every page.
    total_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalSize'})
    # IsDone reports that this is the last page.
    is_done: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isDone'})
    # NextRecordsCursor continues the query on the next page; blank when IsDone
    # is true. Salesforce expires an unused cursor after about 15 minutes.
    next_records_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextRecordsCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'records', _replace_nil_slices(self.records, 1))


QueryRecordsOutput.__annotations__['records'] = tuple[Record, ...] | None
QueryRecordsOutput.__dataclass_fields__['records'].type = tuple[Record, ...] | None
setattr(QueryRecordsOutput, '__module__', 'dex_connectors.salesforce.models')


# GetRecordInput identifies one record and the fields to read.
@dataclass(frozen=True, slots=True)
class GetRecordInput:
    # SObjectType is the record's object API name, such as Contact or Invoice__c.
    s_object_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'sObjectType'})
    # RecordID is the 15- or 18-character Salesforce record ID.
    record_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'recordId'})
    # Fields lists from 1 to 200 field API names to read, such as Name and
    # Email. Relationship paths are not accepted; query them with queryRecords.
    fields: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'fields', _replace_nil_slices(self.fields, 1))


GetRecordInput.__annotations__['fields'] = tuple[str, ...] | None
GetRecordInput.__dataclass_fields__['fields'].type = tuple[str, ...] | None
setattr(GetRecordInput, '__module__', 'dex_connectors.salesforce.models')


# UpsertRecordByExternalIDInput identifies one record by an External ID field
# value and the fields to write to it.
#
# Salesforce creates the record when no record carries ExternalIDValue in
# ExternalIDField and updates it when exactly one does. Repeating the same
# input therefore converges on one record, which is what makes a retried Step
# safe; mark the field Unique as well as External ID so that concurrent
# attempts cannot create two records.
@dataclass(frozen=True, slots=True)
class UpsertRecordByExternalIDInput:
    # SObjectType is the object API name, such as Contact or Invoice__c.
    s_object_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'sObjectType'})
    # ExternalIDField is the API name of a field marked External ID, such as ERP_Id__c.
    external_id_field: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalIdField'})
    # ExternalIDValue is the correlation key from the other system, at most 255 characters.
    external_id_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'externalIdValue'})
    # Fields maps field API names to exact JSON values, such as "LastName": "Raman".
    # It must not set Id or ExternalIDField, which the request path names.
    fields: dict[str, Any] | None = None


setattr(UpsertRecordByExternalIDInput, '__module__', 'dex_connectors.salesforce.models')


# ProviderError is one machine-readable Salesforce REST error. It keeps the
# error code and the field API names Salesforce reported, never the message
# text, which can repeat record values.
@dataclass(frozen=True, slots=True)
class ProviderError:
    # ErrorCode is Salesforce's error code, such as REQUIRED_FIELD_MISSING.
    error_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'errorCode'})
    # Fields lists the field API names Salesforce associated with the error, such as LastName.
    fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(ProviderError, '__module__', 'dex_connectors.salesforce.models')


# UpsertRecordByExternalIDOutput reports the upserted record or why Salesforce refused it.
@dataclass(frozen=True, slots=True)
class UpsertRecordByExternalIDOutput:
    # ID is the 18-character ID of the created or updated record on upserted.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # IsCreated reports that this request created the record. A retried Step
    # can report false after an earlier attempt created it.
    is_created: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCreated', 'dex_json_omit_default': True})
    # MatchingRecordIDs lists the records that share the external ID value on multipleMatches.
    matching_record_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'matchingRecordIds', 'dex_json_omit_default': True})
    # ProviderErrors lists Salesforce's error codes and fields on recordRejected and providerRejected.
    provider_errors: tuple[ProviderError, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'providerErrors', 'dex_json_omit_default': True})


setattr(UpsertRecordByExternalIDOutput, '__module__', 'dex_connectors.salesforce.models')


# UpdateRecordInput identifies one record and the fields to change.
@dataclass(frozen=True, slots=True)
class UpdateRecordInput:
    # SObjectType is the record's object API name, such as Opportunity.
    s_object_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'sObjectType'})
    # RecordID is the 15- or 18-character Salesforce record ID.
    record_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'recordId'})
    # Fields maps at least one field API name to its exact JSON value, such as
    # "StageName": "Closed Won"; JSON null clears a field. Fields that are not
    # listed keep their values.
    fields: dict[str, Any] | None = None


setattr(UpdateRecordInput, '__module__', 'dex_connectors.salesforce.models')


# UpdateRecordOutput reports the updated record or why Salesforce refused the change.
@dataclass(frozen=True, slots=True)
class UpdateRecordOutput:
    # ID is the updated record's ID on updated, as given in the input.
    id: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ProviderErrors lists Salesforce's error codes and fields on recordRejected and providerRejected.
    provider_errors: tuple[ProviderError, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'providerErrors', 'dex_json_omit_default': True})


setattr(UpdateRecordOutput, '__module__', 'dex_connectors.salesforce.models')
