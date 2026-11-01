# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa
"""Generated Python dataclasses from Go operation types."""

from dataclasses import dataclass, field as _dataclass_field

@dataclass(frozen=True, slots=True)
class ListSitesInput:
    """Typed ListSitesInput value from the Go connector contract."""

    pass


setattr(ListSitesInput, '__module__', 'dex_connectors.netlify.models')


@dataclass(frozen=True, slots=True)
class Site:
    """Typed Site value from the Go connector contract."""

    id: str = ''
    name: str = ''
    url: str = ''
    admin_url: str = ''


setattr(Site, '__module__', 'dex_connectors.netlify.models')


@dataclass(frozen=True, slots=True)
class SitesPage:
    """Typed SitesPage value from the Go connector contract."""

    sites: tuple[Site, ...] | None = None


setattr(SitesPage, '__module__', 'dex_connectors.netlify.models')


@dataclass(frozen=True, slots=True)
class ListDeploysInput:
    """Typed ListDeploysInput value from the Go connector contract."""

    site_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'siteId'})


setattr(ListDeploysInput, '__module__', 'dex_connectors.netlify.models')


@dataclass(frozen=True, slots=True)
class Deploy:
    """Typed Deploy value from the Go connector contract."""

    id: str = ''
    site_id: str = ''
    state: str = ''
    url: str = ''


setattr(Deploy, '__module__', 'dex_connectors.netlify.models')


@dataclass(frozen=True, slots=True)
class DeploysPage:
    """Typed DeploysPage value from the Go connector contract."""

    deploys: tuple[Deploy, ...] | None = None


setattr(DeploysPage, '__module__', 'dex_connectors.netlify.models')
