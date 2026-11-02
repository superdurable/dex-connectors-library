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


# ListSitesInput requests the default Netlify sites page.
@dataclass(frozen=True, slots=True)
class ListSitesInput:
    pass


setattr(ListSitesInput, '__module__', 'dex_connectors.netlify.models')


# Site is a Netlify site summary.
@dataclass(frozen=True, slots=True)
class Site:
    # ID is the API site identifier, shown as Project ID in Netlify.
    id: str = ''
    # Name is the site name.
    name: str = ''
    # URL is the site's public URL.
    url: str = ''
    # AdminURL links to the Netlify management page.
    admin_url: str = ''


setattr(Site, '__module__', 'dex_connectors.netlify.models')


# SitesPage contains the returned sites.
@dataclass(frozen=True, slots=True)
class SitesPage:
    # Sites contains one provider page, possibly empty.
    sites: tuple[Site, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'sites', _replace_nil_slices(self.sites, 1))


SitesPage.__annotations__['sites'] = tuple[Site, ...] | None
SitesPage.__dataclass_fields__['sites'].type = tuple[Site, ...] | None
setattr(SitesPage, '__module__', 'dex_connectors.netlify.models')


# ListDeploysInput names the site whose deploys are listed.
@dataclass(frozen=True, slots=True)
class ListDeploysInput:
    # SiteID is the API site ID or accepted site domain.
    site_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'siteId'})


setattr(ListDeploysInput, '__module__', 'dex_connectors.netlify.models')


# Deploy is a Netlify deploy summary.
@dataclass(frozen=True, slots=True)
class Deploy:
    # ID is the deployment identifier.
    id: str = ''
    # SiteID identifies its Netlify site.
    site_id: str = ''
    # State is the provider-reported deployment state.
    state: str = ''
    # URL is the deployment URL.
    url: str = ''


setattr(Deploy, '__module__', 'dex_connectors.netlify.models')


# DeploysPage contains the returned deploys.
@dataclass(frozen=True, slots=True)
class DeploysPage:
    # Deploys contains one provider page, possibly empty.
    deploys: tuple[Deploy, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'deploys', _replace_nil_slices(self.deploys, 1))


DeploysPage.__annotations__['deploys'] = tuple[Deploy, ...] | None
DeploysPage.__dataclass_fields__['deploys'].type = tuple[Deploy, ...] | None
setattr(DeploysPage, '__module__', 'dex_connectors.netlify.models')
