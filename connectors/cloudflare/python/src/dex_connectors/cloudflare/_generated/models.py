# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListZonesInput:
    """Typed ListZonesInput value from the Go connector contract."""

    pass


setattr(ListZonesInput, '__module__', 'dex_connectors.cloudflare.models')


@dataclass(frozen=True, slots=True)
class Zone:
    """Typed Zone value from the Go connector contract."""

    id: str = ''
    name: str = ''
    status: str = ''


setattr(Zone, '__module__', 'dex_connectors.cloudflare.models')


@dataclass(frozen=True, slots=True)
class ZonesPage:
    """Typed ZonesPage value from the Go connector contract."""

    success: bool = False
    result: tuple[Zone, ...] | None = None


setattr(ZonesPage, '__module__', 'dex_connectors.cloudflare.models')


@dataclass(frozen=True, slots=True)
class PurgeCacheByURLsInput:
    """Typed PurgeCacheByURLsInput value from the Go connector contract."""

    zone_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'zoneId'})
    urls: tuple[str, ...] | None = None


setattr(PurgeCacheByURLsInput, '__module__', 'dex_connectors.cloudflare.models')


@dataclass(frozen=True, slots=True)
class CachePurgeResult:
    """Typed CachePurgeResult value from the Go connector contract."""

    id: str = ''


setattr(CachePurgeResult, '__module__', 'dex_connectors.cloudflare.models')


@dataclass(frozen=True, slots=True)
class CachePurge:
    """Typed CachePurge value from the Go connector contract."""

    success: bool = False
    result: CachePurgeResult = _dataclass_field(default_factory=CachePurgeResult)


setattr(CachePurge, '__module__', 'dex_connectors.cloudflare.models')
