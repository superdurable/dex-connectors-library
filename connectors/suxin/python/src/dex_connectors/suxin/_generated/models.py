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


# ListOrganizationsInput selects the API user's accessible organizations without additional user input.
@dataclass(frozen=True, slots=True)
class ListOrganizationsInput:
    pass


setattr(ListOrganizationsInput, '__module__', 'dex_connectors.suxin.models')


# OrganizationSummary excludes registration numbers and personal details.
@dataclass(frozen=True, slots=True)
class OrganizationSummary:
    # ID is the provider's organization ID.
    id: str = ''
    # Name is the organization's name.
    name: str = ''
    # EntityType distinguishes a company from a self-employed entity.
    entity_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'entityType'})
    # CertifyStatus is the provider's certification status.
    certify_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'certifyStatus'})


setattr(OrganizationSummary, '__module__', 'dex_connectors.suxin.models')


# ListOrganizationsOutput contains the unpaginated organization list returned by Suxin.
@dataclass(frozen=True, slots=True)
class ListOrganizationsOutput:
    organizations: tuple[OrganizationSummary, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'organizations', _replace_nil_slices(self.organizations, 1))


ListOrganizationsOutput.__annotations__['organizations'] = tuple[OrganizationSummary, ...] | None
ListOrganizationsOutput.__dataclass_fields__['organizations'].type = tuple[OrganizationSummary, ...] | None
setattr(ListOrganizationsOutput, '__module__', 'dex_connectors.suxin.models')
