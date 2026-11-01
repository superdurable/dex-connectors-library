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


# SearchPhotosInput selects one page of public photos.
@dataclass(frozen=True, slots=True)
class SearchPhotosInput:
    # Query is the nonblank search phrase.
    query: str = ''
    # Page is the 1-based page; zero uses Unsplash's first page.
    page: int = _dataclass_field(default=0, metadata={'dex_json_omit_default': True})
    # PerPage is the page size; zero uses Unsplash's default of ten.
    per_page: int = _dataclass_field(default=0, metadata={'dex_json_name': 'perPage', 'dex_json_omit_default': True})


setattr(SearchPhotosInput, '__module__', 'dex_connectors.unsplash.models')


# Photo contains hotlink and attribution fields for one Unsplash image.
@dataclass(frozen=True, slots=True)
class Photo:
    # ID is the stable Unsplash photo ID.
    id: str = ''
    # Description is the photographer's description, when present.
    description: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # AltDescription is Unsplash's suggested alternative text, when present.
    alt_description: str | None = _dataclass_field(default=None, metadata={'dex_json_name': 'altDescription', 'dex_json_omit_default': True})
    # HTMLURL is the photo's Unsplash page for attribution.
    html_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlUrl'})
    # RegularURL is the Unsplash hosted image URL to hotlink.
    regular_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'regularUrl'})
    # PhotographerName is the credited photographer.
    photographer_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'photographerName'})
    # PhotographerURL is the photographer's Unsplash profile for attribution.
    photographer_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'photographerURL'})


setattr(Photo, '__module__', 'dex_connectors.unsplash.models')


# PhotoPage is one search page and its provider counts.
@dataclass(frozen=True, slots=True)
class PhotoPage:
    # Total is the number of matching photos.
    total: int = 0
    # TotalPages is the number of available pages.
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})
    # Photos holds this page's photos.
    photos: tuple[Photo, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'photos', _replace_nil_slices(self.photos, 1))


PhotoPage.__annotations__['photos'] = tuple[Photo, ...] | None
PhotoPage.__dataclass_fields__['photos'].type = tuple[Photo, ...] | None
setattr(PhotoPage, '__module__', 'dex_connectors.unsplash.models')


# GetPhotoInput identifies a public photo.
@dataclass(frozen=True, slots=True)
class GetPhotoInput:
    # ID is the Unsplash photo ID.
    id: str = ''


setattr(GetPhotoInput, '__module__', 'dex_connectors.unsplash.models')
