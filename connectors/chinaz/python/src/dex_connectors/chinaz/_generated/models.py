# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field

# GetSiteTDKInput selects one public website by domain name.
@dataclass(frozen=True, slots=True)
class GetSiteTDKInput:
    # Domain is a hostname without scheme, port, or path.
    domain: str = ''


setattr(GetSiteTDKInput, '__module__', 'dex_connectors.chinaz.models')


# SiteTDK contains selected public metadata from the Chinaz site index.
@dataclass(frozen=True, slots=True)
class SiteTDK:
    # Domain is the requested domain name.
    domain: str = ''
    # Title is the indexed site title.
    title: str = ''
    # Keywords is the indexed site keyword text.
    keywords: str = ''
    # Description is the indexed site description.
    description: str = ''
    # IPAddress is the provider-reported site IP address.
    ip_address: str = _dataclass_field(default='', metadata={'dex_json_name': 'ipAddress'})


setattr(SiteTDK, '__module__', 'dex_connectors.chinaz.models')
