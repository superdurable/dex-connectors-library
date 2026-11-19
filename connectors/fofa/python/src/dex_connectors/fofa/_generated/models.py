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


# SearchDomainAssetsInput identifies one DNS domain to search in FOFA's public index.
@dataclass(frozen=True, slots=True)
class SearchDomainAssetsInput:
    # Domain is one DNS domain, without a URL scheme or wildcard.
    domain: str = ''


setattr(SearchDomainAssetsInput, '__module__', 'dex_connectors.fofa.models')


# DomainAsset is one public host/IP/port record.
@dataclass(frozen=True, slots=True)
class DomainAsset:
    # Host is the indexed hostname or URL.
    host: str = ''
    # IPAddress is the indexed IPv4 or IPv6 address.
    ip_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'ipAddress'})
    # Port is the indexed TCP or UDP port.
    port: int = 0


setattr(DomainAsset, '__module__', 'dex_connectors.fofa.models')


# SearchDomainAssetsOutput contains the total matches and up to five assets.
@dataclass(frozen=True, slots=True)
class SearchDomainAssetsOutput:
    # Total is FOFA's reported count for the domain.
    total: int = 0
    # Assets are at most five records from the first page.
    assets: tuple[DomainAsset, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'assets', _replace_nil_slices(self.assets, 1))


SearchDomainAssetsOutput.__annotations__['assets'] = tuple[DomainAsset, ...] | None
SearchDomainAssetsOutput.__dataclass_fields__['assets'].type = tuple[DomainAsset, ...] | None
setattr(SearchDomainAssetsOutput, '__module__', 'dex_connectors.fofa.models')
