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


# SearchObjectsInput selects one bounded page of partial search results.
@dataclass(frozen=True, slots=True)
class SearchObjectsInput:
    # ObjectType is the object type to search.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # Term is the search text, at least 2 characters, or 1 with ExactMatch.
    term: str = ''
    # Fields limits where Term is searched; empty searches every field the object type supports.
    fields: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # ExactMatch returns only records whose field equals Term, ignoring case.
    exact_match: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'exactMatch', 'dex_json_omit_default': True})
    # OrganizationID limits a persons or deals search to one organization.
    organization_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationId', 'dex_json_omit_default': True})
    # PersonID limits a deals search to one person.
    person_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'personId', 'dex_json_omit_default': True})
    # Statuses limits a deals search to open, won, or lost deals.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Limit is the page size from 1 through 500; zero uses 25.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Cursor is the NextCursor of the previous page; empty starts at the first result.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SearchObjectsInput, '__module__', 'dex_connectors.pipedrive.models')


# CRMObject is one Pipedrive person, organization, or deal. The typed fields
# expose the association graph, owner, and stage as decimal ID strings; Fields
# and CustomFields keep every returned value exactly as Pipedrive sent it.
@dataclass(frozen=True, slots=True)
class CRMObject:
    # ObjectType is the record's object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # ID is Pipedrive's positive integer record ID as a decimal string.
    id: str = ''
    # Name is a person's or organization's name, or a deal's title.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # OwnerID is the owning Pipedrive user's ID, when set.
    owner_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'ownerId', 'dex_json_omit_default': True})
    # OrganizationID is the linked organization of a person or deal, when set.
    organization_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationId', 'dex_json_omit_default': True})
    # PersonID is the linked person of a deal, when set.
    person_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'personId', 'dex_json_omit_default': True})
    # PipelineID is a deal's pipeline.
    pipeline_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pipelineId', 'dex_json_omit_default': True})
    # StageID is a deal's stage.
    stage_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'stageId', 'dex_json_omit_default': True})
    # Status is a deal's Pipedrive status, such as open, won, or lost.
    status: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Emails lists a person's email addresses, primary first when Pipedrive marks one.
    emails: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # AddTime is when Pipedrive created the record, when reported.
    add_time: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'addTime', 'dex_json_omit_default': True})
    # UpdateTime is when Pipedrive last changed the record, when reported.
    update_time: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'updateTime', 'dex_json_omit_default': True})
    # IsPartial reports a search result, which carries only Pipedrive's search
    # summary; hydrate it with getObject.
    is_partial: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isPartial', 'dex_json_omit_default': True})
    # Fields maps every returned standard field name to its exact JSON value.
    fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # CustomFields maps each returned custom field's 40-character key to its
    # exact JSON value; a search result lists none.
    custom_fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.add_time is not None:
            timestamp = self.add_time
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'add_time'))
            object.__setattr__(self, 'add_time', canonical)
        if self.update_time is not None:
            timestamp = self.update_time
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'update_time'))
            object.__setattr__(self, 'update_time', canonical)


setattr(CRMObject, '__module__', 'dex_connectors.pipedrive.models')


# ObjectPage is one bounded page of search or list results.
@dataclass(frozen=True, slots=True)
class ObjectPage:
    # ObjectType is the searched or listed object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # Objects are the records on this page, possibly none.
    objects: tuple[CRMObject, ...] = ()
    # NextCursor is the opaque cursor for the next page, empty on the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'objects', _replace_nil_slices(self.objects, 1))


ObjectPage.__annotations__['objects'] = tuple[CRMObject, ...] | None
ObjectPage.__dataclass_fields__['objects'].type = tuple[CRMObject, ...] | None
setattr(ObjectPage, '__module__', 'dex_connectors.pipedrive.models')


# GetObjectInput identifies one record.
@dataclass(frozen=True, slots=True)
class GetObjectInput:
    # ObjectType is the record's object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # ObjectID is Pipedrive's positive decimal record ID.
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectId'})


setattr(GetObjectInput, '__module__', 'dex_connectors.pipedrive.models')


# ListObjectsInput selects one bounded page of full records. Every filter is
# optional; filters combine with AND.
@dataclass(frozen=True, slots=True)
class ListObjectsInput:
    # ObjectType is the object type to list.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # UpdatedSince keeps records whose update_time is at or after this time; zero sets no lower bound.
    updated_since: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'updatedSince', 'dex_json_omit_default': True})
    # UpdatedUntil keeps records whose update_time is before this time; zero sets no upper bound.
    updated_until: str = _dataclass_field(default='0001-01-01T00:00:00Z', metadata={'dex_json_name': 'updatedUntil', 'dex_json_omit_default': True})
    # OwnerID keeps records owned by one Pipedrive user.
    owner_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'ownerId', 'dex_json_omit_default': True})
    # OrganizationID keeps persons or deals linked to one organization.
    organization_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizationId', 'dex_json_omit_default': True})
    # PersonID keeps deals linked to one person.
    person_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'personId', 'dex_json_omit_default': True})
    # PipelineID keeps deals in one pipeline.
    pipeline_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'pipelineId', 'dex_json_omit_default': True})
    # StageID keeps deals in one stage.
    stage_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'stageId', 'dex_json_omit_default': True})
    # Statuses keeps deals with one of these statuses; empty lists every deal that is not deleted.
    statuses: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # CustomFieldKeys limits returned custom fields to at most 15 keys; empty returns all of them.
    custom_field_keys: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'customFieldKeys', 'dex_json_omit_default': True})
    # SortBy orders the page; empty uses id, or update_time when UpdatedSince is set.
    sort_by: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sortBy', 'dex_json_omit_default': True})
    # SortDirection orders ascending or descending; empty is ascending.
    sort_direction: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'sortDirection', 'dex_json_omit_default': True})
    # Limit is the page size from 1 through 500; zero uses 25.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # Cursor is the NextCursor of the previous page; empty starts at the first record.
    cursor: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if self.updated_since is not None:
            timestamp = self.updated_since
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'updated_since'))
            object.__setattr__(self, 'updated_since', canonical)
        if self.updated_until is not None:
            timestamp = self.updated_until
            canonical = (format_rfc3339_nano(timestamp) if isinstance(timestamp, datetime) else canonical_go_timestamp(timestamp, 'updated_until'))
            object.__setattr__(self, 'updated_until', canonical)


setattr(ListObjectsInput, '__module__', 'dex_connectors.pipedrive.models')


# UpsertObjectInput creates or updates one person identified by email, or one
# organization identified by name. Pipedrive does not enforce either value as
# unique, so more than one match selects multipleMatches and writes nothing.
@dataclass(frozen=True, slots=True)
class UpsertObjectInput:
    # ObjectType is persons or organizations; deals have no identity value, so use createObject.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # IDProperty is email for persons or name for organizations.
    id_property: str = _dataclass_field(default='', metadata={'dex_json_name': 'idProperty'})
    # IDValue is the email address or the organization name. Pipedrive's exact
    # search ignores case, and so does the connector's match.
    id_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'idValue'})
    # Fields maps standard field names to JSON values written on create and on
    # update. A person requires name; neither may set the identity field
    # itself, which the connector writes on create.
    fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # CustomFields maps 40-character custom field keys to JSON values written on create and on update.
    custom_fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})


setattr(UpsertObjectInput, '__module__', 'dex_connectors.pipedrive.models')


# UpsertedObject is the record an upsert created or updated.
@dataclass(frozen=True, slots=True)
class UpsertedObject:
    # Object is the record with the values Pipedrive returned.
    object: CRMObject
    # Created reports whether this attempt created the record. After a retry
    # that found the record an earlier attempt created, it is false.
    created: bool = False
    # MatchingIDs lists the records that carry the identity value on multipleMatches.
    matching_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'matchingIds', 'dex_json_omit_default': True})


setattr(UpsertedObject, '__module__', 'dex_connectors.pipedrive.models')


# CreateObjectInput is one new record. Fields use Pipedrive API v2 field
# names, such as title, person_id, org_id, stage_id, owner_id, and value for a
# deal, or name, org_id, and emails for a person; each value is exact JSON.
@dataclass(frozen=True, slots=True)
class CreateObjectInput:
    # ObjectType is the type of record to create.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # Fields maps standard field names to JSON values. A deal requires title;
    # a person or organization requires name.
    fields: dict[str, Any] | None = None
    # CustomFields maps custom field keys, the 40-character keys shown in
    # Company settings > Data fields, to JSON values.
    custom_fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})


setattr(CreateObjectInput, '__module__', 'dex_connectors.pipedrive.models')


# UpdateObjectInput sets named fields on one record. Fields that are not named keep their values.
@dataclass(frozen=True, slots=True)
class UpdateObjectInput:
    # ObjectType is the record's object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # ObjectID is Pipedrive's positive decimal record ID.
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectId'})
    # Fields maps standard field names, such as stage_id or owner_id, to JSON
    # values; JSON null clears a field that Pipedrive allows to be empty.
    fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # CustomFields maps 40-character custom field keys to JSON values; JSON null clears a value.
    custom_fields: dict[str, Any] = _dataclass_field(default_factory=dict, metadata={'dex_json_name': 'customFields', 'dex_json_omit_default': True})


setattr(UpdateObjectInput, '__module__', 'dex_connectors.pipedrive.models')
