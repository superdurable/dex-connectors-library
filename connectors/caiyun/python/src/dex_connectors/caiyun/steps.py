# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Caiyun Step factory."""

from ._generated.steps import new_get_current_weather_step as new_get_current_weather_step

new_get_current_weather_step.__doc__ = "Create the Caiyun Query Step; unwired optional branches fail."
