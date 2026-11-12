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


# ListFeaturesInput selects a page by its opaque, non-secret cursor.
@dataclass(frozen=True, slots=True)
class ListFeaturesInput:
    # PageCursor is blank for the first page or a previous NextCursor value.
    page_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageCursor', 'dex_json_omit_default': True})


setattr(ListFeaturesInput, '__module__', 'dex_connectors.productboard.models')


# FeatureMetadata contains only the feature ID and name.
@dataclass(frozen=True, slots=True)
class FeatureMetadata:
    # ID is the Productboard entity ID.
    id: str = ''
    # Name is the feature name.
    name: str = ''


setattr(FeatureMetadata, '__module__', 'dex_connectors.productboard.models')


# FeaturesPage contains one page and the next opaque cursor, if any.
@dataclass(frozen=True, slots=True)
class FeaturesPage:
    # Features excludes descriptions and other provider fields.
    features: tuple[FeatureMetadata, ...] = ()
    # NextCursor is blank at the last page.
    next_cursor: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextCursor'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'features', _replace_nil_slices(self.features, 1))


FeaturesPage.__annotations__['features'] = tuple[FeatureMetadata, ...] | None
FeaturesPage.__dataclass_fields__['features'].type = tuple[FeatureMetadata, ...] | None
setattr(FeaturesPage, '__module__', 'dex_connectors.productboard.models')
