# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from dataclasses import dataclass

from ._generated.models import (
    Deploy as Deploy,
)
from ._generated.models import (
    ListDeploysInput as ListDeploysInput,
)
from ._generated.models import (
    ListSitesInput as ListSitesInput,
)
from ._generated.models import (
    Site as Site,
)


@dataclass(frozen=True, slots=True)
class SitesPage:
    """Sites visible to the token."""

    sites: tuple[Site, ...] = ()


@dataclass(frozen=True, slots=True)
class DeploysPage:
    """Deploys for the site."""

    deploys: tuple[Deploy, ...] = ()
