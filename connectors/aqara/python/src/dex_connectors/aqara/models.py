# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Public Aqara models generated from the Go connector contract."""

from ._generated.models import Device as Device
from ._generated.models import DevicePage as DevicePage
from ._generated.models import ListDevicesInput as ListDevicesInput

Device.__doc__ = "One authorized Aqara device and its reported state."
DevicePage.__doc__ = "One bounded page of devices and total matching count."
ListDevicesInput.__doc__ = "Select a one-based device page and size."
