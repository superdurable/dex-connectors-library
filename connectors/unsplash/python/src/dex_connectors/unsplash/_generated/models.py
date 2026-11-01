# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class SearchPhotosInput:
    """Typed SearchPhotosInput value from the Go connector contract."""

    query: str = ''
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(SearchPhotosInput, '__module__', 'dex_connectors.unsplash.models')


@dataclass(frozen=True, slots=True)
class Photo:
    """Typed Photo value from the Go connector contract."""

    id: str = ''
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    alt_description: str = _dataclass_field(default='', metadata={'dex_json_name': 'altDescription', 'dex_json_omit_default': True})
    html_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlUrl'})
    regular_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'regularUrl'})
    photographer_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'photographerName'})
    photographer_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'photographerURL'})


setattr(Photo, '__module__', 'dex_connectors.unsplash.models')


@dataclass(frozen=True, slots=True)
class PhotoPage:
    """Typed PhotoPage value from the Go connector contract."""

    total: int = 0
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})
    photos: tuple[Photo, ...] | None = None


setattr(PhotoPage, '__module__', 'dex_connectors.unsplash.models')


@dataclass(frozen=True, slots=True)
class GetPhotoInput:
    """Typed GetPhotoInput value from the Go connector contract."""

    id: str = ''


setattr(GetPhotoInput, '__module__', 'dex_connectors.unsplash.models')
