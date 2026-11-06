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


# SearchFilter is one typed CRM search condition.
@dataclass(frozen=True, slots=True)
class SearchFilter:
    # PropertyName is an internal property name such as email, or an
    # association pseudo-property such as associations.contact.
    property_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'propertyName'})
    # Operator selects the comparison.
    operator: str = _dataclass_field(kw_only=True)
    # Value is the comparison value for EQ, NEQ, LT, LTE, GT, GTE, BETWEEN,
    # CONTAINS_TOKEN, and NOT_CONTAINS_TOKEN. Dates are Unix milliseconds.
    value: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # HighValue is the inclusive upper bound for BETWEEN.
    high_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'highValue', 'dex_json_omit_default': True})
    # Values lists the candidates for IN and NOT_IN. HubSpot requires lowercase
    # values when the property is a string property.
    values: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(SearchFilter, '__module__', 'dex_connectors.hubspot.models')


# SearchFilterGroup AND-s its filters. A search OR-s its groups.
@dataclass(frozen=True, slots=True)
class SearchFilterGroup:
    # Filters are the conditions that must all match.
    filters: tuple[SearchFilter, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'filters', _replace_nil_slices(self.filters, 1))


SearchFilterGroup.__annotations__['filters'] = tuple[SearchFilter, ...] | None
SearchFilterGroup.__dataclass_fields__['filters'].type = tuple[SearchFilter, ...] | None
setattr(SearchFilterGroup, '__module__', 'dex_connectors.hubspot.models')


# SearchSort orders search results by one property.
@dataclass(frozen=True, slots=True)
class SearchSort:
    # PropertyName is the internal property name to sort by.
    property_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'propertyName'})
    # Direction is ascending or descending.
    direction: str = _dataclass_field(kw_only=True)


setattr(SearchSort, '__module__', 'dex_connectors.hubspot.models')


# SearchObjectsInput selects one bounded page of matching records.
@dataclass(frozen=True, slots=True)
class SearchObjectsInput:
    # ObjectType is the object type to search.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # FilterGroups are OR-ed; empty matches every record. HubSpot allows five
    # groups, six filters per group, and eighteen filters in total.
    filter_groups: tuple[SearchFilterGroup, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'filterGroups', 'dex_json_omit_default': True})
    # Query optionally searches the object's default text properties.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Sort optionally orders results; without it HubSpot returns oldest records first.
    sort: SearchSort | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Properties lists the property values to return; empty returns HubSpot's
    # default properties for the object type.
    properties: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # Limit is the page size from 1 through 200; zero uses 10.
    limit: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # After is the NextAfter cursor of the previous page; empty starts at the
    # first result. Paging cannot pass HubSpot's 10,000th result.
    after: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(SearchObjectsInput, '__module__', 'dex_connectors.hubspot.models')


# CRMObject is one HubSpot CRM record.
@dataclass(frozen=True, slots=True)
class CRMObject:
    # ObjectType is the record's object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # ID is HubSpot's record ID (hs_object_id), a decimal string.
    id: str = ''
    # Properties holds the returned property values keyed by internal property
    # name. HubSpot returns every value as a string; a property without a value
    # is absent.
    properties: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})
    # CreatedAt is when HubSpot created the record, when reported.
    created_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'createdAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # UpdatedAt is when HubSpot last modified the record, when reported.
    updated_at: datetime = _dataclass_field(default_factory=lambda: datetime.min.replace(tzinfo=UTC), metadata={'dex_json_name': 'updatedAt', 'dex_json_omit_default': True, 'dex_json_format': 'rfc3339'})
    # Archived reports whether the record is archived.
    archived: bool = False
    # URL is the HubSpot app link to the record when HubSpot returns one.
    url: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        if isinstance(self.created_at, str):
            object.__setattr__(self, 'created_at', parse_go_datetime(self.created_at, 'created_at'))
        elif self.created_at is not None:
            validate_go_datetime(self.created_at, 'created_at')
        if isinstance(self.updated_at, str):
            object.__setattr__(self, 'updated_at', parse_go_datetime(self.updated_at, 'updated_at'))
        elif self.updated_at is not None:
            validate_go_datetime(self.updated_at, 'updated_at')


setattr(CRMObject, '__module__', 'dex_connectors.hubspot.models')


# ObjectPage is one page of CRM search results.
@dataclass(frozen=True, slots=True)
class ObjectPage:
    # ObjectType is the searched object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # Total is HubSpot's count of all matching records.
    total: int = 0
    # Objects are the records on this page, possibly none.
    objects: tuple[CRMObject, ...] = ()
    # NextAfter is the cursor for the next page, empty on the last page.
    next_after: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextAfter', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'objects', _replace_nil_slices(self.objects, 1))


ObjectPage.__annotations__['objects'] = tuple[CRMObject, ...] | None
ObjectPage.__dataclass_fields__['objects'].type = tuple[CRMObject, ...] | None
setattr(ObjectPage, '__module__', 'dex_connectors.hubspot.models')


# GetObjectInput identifies one record.
@dataclass(frozen=True, slots=True)
class GetObjectInput:
    # ObjectType is the record's object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # ObjectID is HubSpot's decimal record ID.
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectId'})
    # Properties lists the property values to return; empty returns HubSpot's
    # default properties for the object type.
    properties: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})


setattr(GetObjectInput, '__module__', 'dex_connectors.hubspot.models')


# UpsertObjectInput creates or updates one record identified by a unique property value.
@dataclass(frozen=True, slots=True)
class UpsertObjectInput:
    # ObjectType is the record's object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # IDProperty is a unique identifier property: email for contacts, or a
    # custom property created with unique values for any object type.
    id_property: str = _dataclass_field(default='', metadata={'dex_json_name': 'idProperty'})
    # IDValue is the unique value that identifies the record, such as the email address.
    id_value: str = _dataclass_field(default='', metadata={'dex_json_name': 'idValue'})
    # Properties are the values to set, keyed by internal property name. An
    # empty string clears a value.
    properties: dict[str, str] = _dataclass_field(default_factory=dict, metadata={'dex_json_omit_default': True})


setattr(UpsertObjectInput, '__module__', 'dex_connectors.hubspot.models')


# UpsertedObject is the record an upsert created or updated.
@dataclass(frozen=True, slots=True)
class UpsertedObject:
    # Object is the record with the values HubSpot returned.
    object: CRMObject
    # Created reports whether the dispatch HubSpot answered created the record.
    # After a Dex retry or a repeated dispatch it can be false even though an
    # earlier dispatch of the same Step created the record.
    created: bool = False


setattr(UpsertedObject, '__module__', 'dex_connectors.hubspot.models')


# UpdateObjectInput replaces property values on one record.
@dataclass(frozen=True, slots=True)
class UpdateObjectInput:
    # ObjectType is the record's object type.
    object_type: str = _dataclass_field(metadata={'dex_json_name': 'objectType'})
    # ObjectID is HubSpot's decimal record ID.
    object_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'objectId'})
    # Properties are the values to set, keyed by internal property name. At
    # least one is required, and an empty string clears a value.
    properties: dict[str, str] | None = None


setattr(UpdateObjectInput, '__module__', 'dex_connectors.hubspot.models')
