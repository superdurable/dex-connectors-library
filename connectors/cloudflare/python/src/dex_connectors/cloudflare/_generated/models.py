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


# PurgeCacheByURLsInput names one zone and up to 30 absolute URLs.
@dataclass(frozen=True, slots=True)
class PurgeCacheByURLsInput:
    # ZoneID identifies the zone whose cache is purged.
    zone_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'zoneId'})
    # URLs lists absolute HTTP or HTTPS URLs to purge.
    urls: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'urls', _replace_nil_slices(self.urls, 1))


PurgeCacheByURLsInput.__annotations__['urls'] = tuple[str, ...] | None
PurgeCacheByURLsInput.__dataclass_fields__['urls'].type = tuple[str, ...] | None
setattr(PurgeCacheByURLsInput, '__module__', 'dex_connectors.cloudflare.models')


@dataclass(frozen=True, slots=True)
class CachePurgeResult:
    id: str = ''


setattr(CachePurgeResult, '__module__', 'dex_connectors.cloudflare.models')


# CachePurge records Cloudflare's acceptance, not a cache miss guarantee.
@dataclass(frozen=True, slots=True)
class CachePurge:
    # Success reports that Cloudflare accepted the purge request.
    success: bool = False
    # Result contains Cloudflare's purge request ID.
    result: CachePurgeResult = _dataclass_field(default_factory=CachePurgeResult)


setattr(CachePurge, '__module__', 'dex_connectors.cloudflare.models')


# ListZonesInput requests the default Cloudflare zone page.
@dataclass(frozen=True, slots=True)
class ListZonesInput:
    pass


setattr(ListZonesInput, '__module__', 'dex_connectors.cloudflare.models')


# Zone identifies a Cloudflare zone.
@dataclass(frozen=True, slots=True)
class Zone:
    # ID is the zone identifier used by cache operations.
    id: str = ''
    # Name is the zone's DNS name.
    name: str = ''
    # Status is the provider-reported zone status.
    status: str = ''


setattr(Zone, '__module__', 'dex_connectors.cloudflare.models')


# ZonesPage contains Cloudflare zones and success metadata.
@dataclass(frozen=True, slots=True)
class ZonesPage:
    # Success reports whether Cloudflare accepted the list request.
    success: bool = False
    # Result contains one zone page, possibly empty.
    result: tuple[Zone, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'result', _replace_nil_slices(self.result, 1))


ZonesPage.__annotations__['result'] = tuple[Zone, ...] | None
ZonesPage.__dataclass_fields__['result'].type = tuple[Zone, ...] | None
setattr(ZonesPage, '__module__', 'dex_connectors.cloudflare.models')
