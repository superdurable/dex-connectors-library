# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

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
    sites: tuple[Site, ...] | None = None


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
    deploys: tuple[Deploy, ...] | None = None


setattr(DeploysPage, '__module__', 'dex_connectors.netlify.models')
