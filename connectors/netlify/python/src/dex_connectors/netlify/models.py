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

ListSitesInput.__doc__ = "Lists every site the token can see; there are no parameters."
Site.__doc__ = "Site identity, name, public URL, and Netlify admin URL."
ListDeploysInput.__doc__ = "Netlify site ID whose deploys are listed."
Deploy.__doc__ = "Deploy identity, owning site, state such as ready, and deploy URL."
