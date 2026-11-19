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


# ListObjectMetadataInput has no provider-specific query parameters.
@dataclass(frozen=True, slots=True)
class ListObjectMetadataInput:
    pass


setattr(ListObjectMetadataInput, '__module__', 'dex_connectors.fxiaoke.models')


# ObjectMetadata is the safe subset of one CRM object definition.
@dataclass(frozen=True, slots=True)
class ObjectMetadata:
    # APIName is the stable CRM object API name.
    api_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'apiName'})
    # DisplayName is the object's localized display name.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName'})
    # Active indicates whether the object is available.
    active: bool = False


setattr(ObjectMetadata, '__module__', 'dex_connectors.fxiaoke.models')


# ListObjectMetadataOutput contains object names without customer records.
@dataclass(frozen=True, slots=True)
class ListObjectMetadataOutput:
    # Objects are the CRM objects visible to the configured employee.
    objects: tuple[ObjectMetadata, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'objects', _replace_nil_slices(self.objects, 1))


ListObjectMetadataOutput.__annotations__['objects'] = tuple[ObjectMetadata, ...] | None
ListObjectMetadataOutput.__dataclass_fields__['objects'].type = tuple[ObjectMetadata, ...] | None
setattr(ListObjectMetadataOutput, '__module__', 'dex_connectors.fxiaoke.models')
