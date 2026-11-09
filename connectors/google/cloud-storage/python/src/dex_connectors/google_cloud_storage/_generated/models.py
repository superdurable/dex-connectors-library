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


# ListObjectsInput selects one page from a named bucket.
@dataclass(frozen=True, slots=True)
class ListObjectsInput:
    # Bucket is the non-secret bucket name shown in Cloud Storage Buckets.
    bucket: str = ''
    # Prefix limits names to this optional object name prefix.
    prefix: str = ''
    # PageSize is 1 through 1000; zero uses 100.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize'})
    # PageToken is Google's opaque cursor; blank selects the first page.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken'})


setattr(ListObjectsInput, '__module__', 'dex_connectors.google_cloud_storage.models')


# Object describes one object identity and immutable generation.
@dataclass(frozen=True, slots=True)
class Object:
    # Name is the exact object name returned by Google.
    name: str = ''
    # Generation selects a specific immutable object version.
    generation: str = ''


setattr(Object, '__module__', 'dex_connectors.google_cloud_storage.models')


# ObjectsPage contains one bounded page and its next cursor.
@dataclass(frozen=True, slots=True)
class ObjectsPage:
    # Objects are projected object identities.
    objects: tuple[Object, ...] = ()
    # NextPageToken is blank on the final page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'objects', _replace_nil_slices(self.objects, 1))


ObjectsPage.__annotations__['objects'] = tuple[Object, ...] | None
ObjectsPage.__dataclass_fields__['objects'].type = tuple[Object, ...] | None
setattr(ObjectsPage, '__module__', 'dex_connectors.google_cloud_storage.models')


# DownloadObjectInput selects one bounded object body.
@dataclass(frozen=True, slots=True)
class DownloadObjectInput:
    # Bucket is the non-secret bucket name shown in Cloud Storage Buckets.
    bucket: str = ''
    # Name is the exact object name returned by listObjects.
    name: str = ''
    # Generation optionally pins an immutable generation returned by listObjects.
    generation: str = ''


setattr(DownloadObjectInput, '__module__', 'dex_connectors.google_cloud_storage.models')


# DownloadedObject carries bounded bytes encoded for a JSON Flow result.
@dataclass(frozen=True, slots=True)
class DownloadedObject:
    # ContentBase64 is the downloaded object data; decode before use.
    content_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentBase64'})
    # ContentType is the HTTP media type supplied by Google.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})


setattr(DownloadedObject, '__module__', 'dex_connectors.google_cloud_storage.models')


# UploadObjectInput supplies a destination and bounded Base64 content.
@dataclass(frozen=True, slots=True)
class UploadObjectInput:
    # Bucket is the destination bucket name shown in Cloud Storage Buckets.
    bucket: str = ''
    # Name is the exact destination object name to create if absent.
    name: str = ''
    # ContentBase64 is the complete object content encoded as Base64.
    content_base64: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentBase64'})
    # ContentType is a valid HTTP media type, such as text/plain.
    content_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'contentType'})


setattr(UploadObjectInput, '__module__', 'dex_connectors.google_cloud_storage.models')
