# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetOnlineVersionInput identifies one Android package in vivo App Store.
@dataclass(frozen=True, slots=True)
class GetOnlineVersionInput:
    # PackageName is the exact Java package name published in vivo App Store.
    package_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'packageName'})


setattr(GetOnlineVersionInput, '__module__', 'dex_connectors.vivo.models')


# GetOnlineVersionOutput contains published version metadata without app contents.
@dataclass(frozen=True, slots=True)
class GetOnlineVersionOutput:
    # PackageName is the requested Android package name.
    package_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'packageName'})
    # VersionName is the user-visible published version.
    version_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'versionName'})
    # VersionCode is the integer Android version code.
    version_code: int = _dataclass_field(default=0, metadata={'dex_json_name': 'versionCode'})


setattr(GetOnlineVersionOutput, '__module__', 'dex_connectors.vivo.models')
