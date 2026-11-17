# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Baidu Maps Step factory."""

from ._generated.steps import new_geocode_address_step as new_geocode_address_step

new_geocode_address_step.__doc__ = "Create the geocoding Query Step; unwired optional branches fail."
