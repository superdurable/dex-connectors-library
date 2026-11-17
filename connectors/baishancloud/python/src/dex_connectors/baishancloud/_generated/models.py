# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass

# DescribeDomainInput identifies one CDN domain by name.
@dataclass(frozen=True, slots=True)
class DescribeDomainInput:
    # Domain is an ASCII DNS name, without protocol or path.
    domain: str = ''


setattr(DescribeDomainInput, '__module__', 'dex_connectors.baishancloud.models')


# DomainDetail is a CDN domain without its origin or cache configuration.
@dataclass(frozen=True, slots=True)
class DomainDetail:
    # ID is the BaishanCloud domain identifier.
    id: str = ''
    # Domain is the CDN domain name.
    domain: str = ''
    # Type is the acceleration service type.
    type: str = ''
    # Status is the provider's service status.
    status: str = ''
    # CNAME is the DNS alias assigned by BaishanCloud.
    cname: str = ''


setattr(DomainDetail, '__module__', 'dex_connectors.baishancloud.models')
