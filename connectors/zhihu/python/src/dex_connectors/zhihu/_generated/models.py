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


# ListQuotasInput selects all API quotas visible to the Access Secret.
@dataclass(frozen=True, slots=True)
class ListQuotasInput:
    pass


setattr(ListQuotasInput, '__module__', 'dex_connectors.zhihu.models')


# Quota describes one enabled Zhihu API's allocation and usage.
@dataclass(frozen=True, slots=True)
class Quota:
    # APIID is the provider's API category identifier.
    api_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'apiId'})
    # APIName is the provider's category display name.
    api_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'apiName'})
    # TotalQuota is the allocated request count for the current natural day.
    total_quota: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalQuota'})
    # TotalUsed is the consumed request count for the current natural day.
    total_used: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalUsed'})
    # RemainingQuota is the provider's nonnegative remaining count for the current natural day.
    remaining_quota: int = _dataclass_field(default=0, metadata={'dex_json_name': 'remainingQuota'})


setattr(Quota, '__module__', 'dex_connectors.zhihu.models')


# QuotaPage contains account quota metadata and no customer content.
@dataclass(frozen=True, slots=True)
class QuotaPage:
    # Quotas is empty when the account has no visible API allocation.
    quotas: tuple[Quota, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'quotas', _replace_nil_slices(self.quotas, 1))


QuotaPage.__annotations__['quotas'] = tuple[Quota, ...] | None
QuotaPage.__dataclass_fields__['quotas'].type = tuple[Quota, ...] | None
setattr(QuotaPage, '__module__', 'dex_connectors.zhihu.models')
