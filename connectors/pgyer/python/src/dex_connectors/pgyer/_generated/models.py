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


# ListUploadedAppsInput selects one page of apps uploaded by the API Key's account.
@dataclass(frozen=True, slots=True)
class ListUploadedAppsInput:
    # Page is a positive page number; zero asks Pgyer for its first page.
    page: int = 0


setattr(ListUploadedAppsInput, '__module__', 'dex_connectors.pgyer.models')


# UploadedApp describes an app build without download URLs or account details.
@dataclass(frozen=True, slots=True)
class UploadedApp:
    # AppKey identifies the app across versions.
    app_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'appKey'})
    # BuildKey identifies this specific uploaded build.
    build_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'buildKey'})
    # Name is the app's display name.
    name: str = ''
    # BuildType is 1 for iOS or 2 for Android.
    build_type: int = _dataclass_field(default=0, metadata={'dex_json_name': 'buildType'})
    # Version is the app's version string.
    version: str = ''
    # CreatedAt is Pgyer's upload-time string.
    created_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'createdAt'})


setattr(UploadedApp, '__module__', 'dex_connectors.pgyer.models')


# UploadedAppPage contains one bounded page of uploaded app metadata.
@dataclass(frozen=True, slots=True)
class UploadedAppPage:
    # Apps contains at most 100 items from one Pgyer response.
    apps: tuple[UploadedApp, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'apps', _replace_nil_slices(self.apps, 1))


UploadedAppPage.__annotations__['apps'] = tuple[UploadedApp, ...] | None
UploadedAppPage.__dataclass_fields__['apps'].type = tuple[UploadedApp, ...] | None
setattr(UploadedAppPage, '__module__', 'dex_connectors.pgyer.models')
