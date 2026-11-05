# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Teams OAuth refresh uses the generated manifest declaration."""

from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver as ManifestCredentialRefreshDriver

from ._generated.refresh import AUTHORIZATION as AUTHORIZATION
from ._generated.refresh import CREDENTIAL_FIELDS as CREDENTIAL_FIELDS
