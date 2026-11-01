# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Typed Netlify site and deploy pages."""

from dataclasses import dataclass, field


@dataclass(frozen=True, slots=True)
class ListSitesInput:
    """Lists every site the token can see; there are no parameters."""


@dataclass(frozen=True, slots=True)
class Site:
    """Site identity, name, public URL, and Netlify admin URL."""

    id: str = ""
    name: str = ""
    url: str = ""
    admin_url: str = ""


@dataclass(frozen=True, slots=True)
class SitesPage:
    """Sites visible to the token."""

    sites: tuple[Site, ...] = ()


@dataclass(frozen=True, slots=True)
class ListDeploysInput:
    """Netlify site ID whose deploys are listed."""

    site_id: str = field(default="", metadata={"dex_json_name": "siteId"})


@dataclass(frozen=True, slots=True)
class Deploy:
    """Deploy identity, owning site, state such as ready, and deploy URL."""

    id: str = ""
    site_id: str = ""
    state: str = ""
    url: str = ""


@dataclass(frozen=True, slots=True)
class DeploysPage:
    """Deploys for the site."""

    deploys: tuple[Deploy, ...] = ()
