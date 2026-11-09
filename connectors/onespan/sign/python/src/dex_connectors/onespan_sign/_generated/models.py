# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetPackageInput selects a package by its non-secret ID.
@dataclass(frozen=True, slots=True)
class GetPackageInput:
    # PackageID is the ID returned by OneSpan Sign when a package is created.
    package_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'packageId'})


setattr(GetPackageInput, '__module__', 'dex_connectors.onespan_sign.models')


# PackageStatus excludes signer identities and document contents.
@dataclass(frozen=True, slots=True)
class PackageStatus:
    # ID is the OneSpan Sign package ID.
    id: str = ''
    # Name is the package name.
    name: str = ''
    # Status is a OneSpan Sign package state such as COMPLETED.
    status: str = ''


setattr(PackageStatus, '__module__', 'dex_connectors.onespan_sign.models')
