# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public geocoding models generated from the Go connector contract."""

from ._generated.models import GeocodeAddressInput as GeocodeAddressInput
from ._generated.models import GeocodeAddressOutput as GeocodeAddressOutput
from ._generated.models import GeocodedLocation as GeocodedLocation

GeocodeAddressInput.__doc__ = "Structured Chinese address and optional city restriction."
GeocodeAddressOutput.__doc__ = "Zero or more validated Amap location candidates."
GeocodedLocation.__doc__ = "One Amap location with coordinates in longitude, latitude order."
