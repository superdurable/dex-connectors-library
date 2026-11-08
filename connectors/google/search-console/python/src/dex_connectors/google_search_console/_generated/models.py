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


# ListSitesInput requests properties visible to the connected Google user.
@dataclass(frozen=True, slots=True)
class ListSitesInput:
    pass


setattr(ListSitesInput, '__module__', 'dex_connectors.google_search_console.models')


# Site describes one Search Console property and the user's permission.
@dataclass(frozen=True, slots=True)
class Site:
    # SiteURL is a URL-prefix property or a sc-domain property identifier.
    site_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'siteUrl'})
    # PermissionLevel is the Google-issued permission for this user.
    permission_level: str = _dataclass_field(default='', metadata={'dex_json_name': 'permissionLevel'})


setattr(Site, '__module__', 'dex_connectors.google_search_console.models')


# SitesPage contains the bounded set returned by Search Console.
@dataclass(frozen=True, slots=True)
class SitesPage:
    # Sites are the accessible Search Console properties.
    sites: tuple[Site, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'sites', _replace_nil_slices(self.sites, 1))


SitesPage.__annotations__['sites'] = tuple[Site, ...] | None
SitesPage.__dataclass_fields__['sites'].type = tuple[Site, ...] | None
setattr(SitesPage, '__module__', 'dex_connectors.google_search_console.models')
