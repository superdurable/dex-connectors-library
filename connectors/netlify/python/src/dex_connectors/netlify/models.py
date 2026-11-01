# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public model names backed by Go-derived generated dataclasses."""

from ._generated.models import (
    Deploy as Deploy,
)
from ._generated.models import DeploysPage as DeploysPage
from ._generated.models import (
    ListDeploysInput as ListDeploysInput,
)
from ._generated.models import (
    ListSitesInput as ListSitesInput,
)
from ._generated.models import (
    Site as Site,
)
from ._generated.models import SitesPage as SitesPage

SitesPage.__doc__ = "Sites visible to the token."
DeploysPage.__doc__ = "Deploys for the site."
