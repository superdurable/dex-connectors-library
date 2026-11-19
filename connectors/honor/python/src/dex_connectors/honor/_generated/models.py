# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# LookupAppIDInput identifies one Android package owned by the Honor developer account.
@dataclass(frozen=True, slots=True)
class LookupAppIDInput:
    # PackageName is the exact Java package name bound in Honor App Management.
    package_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'packageName'})


setattr(LookupAppIDInput, '__module__', 'dex_connectors.honor.models')


# LookupAppIDOutput contains only the matched package name and app ID.
@dataclass(frozen=True, slots=True)
class LookupAppIDOutput:
    # PackageName is the matched Android package name.
    package_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'packageName'})
    # AppID is Honor's numeric ID; zero indicates no match.
    app_id: int = _dataclass_field(default=0, metadata={'dex_json_name': 'appID'})


setattr(LookupAppIDOutput, '__module__', 'dex_connectors.honor.models')
