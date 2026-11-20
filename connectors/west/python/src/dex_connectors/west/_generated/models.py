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


# ListDomainsInput selects one reseller domain page. Zero page and limit use 1 and 20.
@dataclass(frozen=True, slots=True)
class ListDomainsInput:
    # Page is the one-based page number; zero selects the first page.
    page: int = 0
    # Limit is the number of domains to request, from 1 to 100; zero uses 20.
    limit: int = 0


setattr(ListDomainsInput, '__module__', 'dex_connectors.west.models')


# Domain contains non-personal registrar metadata.
@dataclass(frozen=True, slots=True)
class Domain:
    # Name is the registered domain name.
    name: str = ''
    # ExpiresAt is the provider's expiration date text.
    expires_at: str = _dataclass_field(default='', metadata={'dex_json_name': 'expiresAt'})


setattr(Domain, '__module__', 'dex_connectors.west.models')


# ListDomainsOutput contains one domain page and provider pagination counts.
@dataclass(frozen=True, slots=True)
class ListDomainsOutput:
    # Domains are the returned domains in provider order.
    domains: tuple[Domain, ...] = ()
    # Page is the returned one-based page number.
    page: int = 0
    # TotalPages is the total number of pages reported by West.cn.
    total_pages: int = _dataclass_field(default=0, metadata={'dex_json_name': 'totalPages'})
    # Total is the total number of domains reported by West.cn.
    total: int = 0

    def __post_init__(self) -> None:
        object.__setattr__(self, 'domains', _replace_nil_slices(self.domains, 1))


ListDomainsOutput.__annotations__['domains'] = tuple[Domain, ...] | None
ListDomainsOutput.__dataclass_fields__['domains'].type = tuple[Domain, ...] | None
setattr(ListDomainsOutput, '__module__', 'dex_connectors.west.models')
