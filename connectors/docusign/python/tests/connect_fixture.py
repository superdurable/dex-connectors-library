# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Connect request bodies, signatures, and a project loader shared by the Trigger tests, as in Go's."""

import asyncio
import base64
import hashlib
import hmac
import json
from collections.abc import Mapping
from pathlib import Path
from typing import Any

from dex_connectors.sdk import TriggerContext, TriggerEvent
from dex_connectors.sdk.projectconfig import LoadedProject, load_from_environment

from dex_connectors.docusign import EnvelopeEvent

# Inert, split so secret scanners do not mistake the fixture for a credential.
SENTINEL_HMAC_KEY = "SENTINEL-DOCUSIGN-" + "CONNECT-HMAC-KEY"
TEST_ACCOUNT_ID = "a4ec37d6-1111-2222-3333-143885c220e1"
TEST_ENVELOPE_ID = "93be49ab-1111-2222-3333-f752070d71ec"
CONNECTION_NAME = "esignature"
BINDING_NAME = "outcomes"
MODULE_PATH = "github.com/superdurable/dex-connectors-library/connectors/docusign"


def connect_body(event: str, envelope_id: str, request_id: str) -> str:
    """Return a JSON SIM envelope event whose summary includes custom fields, byte for byte as Go's test."""
    return (
        '{"event":"'
        + event
        + '","apiVersion":"v2.1","uri":"/restapi/v2.1/accounts/'
        + TEST_ACCOUNT_ID
        + "/envelopes/"
        + envelope_id
        + '",'
        '"retryCount":0,"configurationId":10242474,"generatedDateTime":"2026-10-04T13:00:00.1230000Z",'
        '"data":{"accountId":"' + TEST_ACCOUNT_ID + '","userId":"4799e5e9-0000-0000-0000-cf4713bbcacc",'
        '"envelopeId":"' + envelope_id + '",'
        '"envelopeSummary":{"status":"' + event.removeprefix("envelope-") + '","emailSubject":"Meridian MSA",'
        '"voidedReason":"Signer left the company","customFields":{"textCustomFields":['
        '{"fieldId":"1","name":"dexSigningRequestId","show":"false","value":"' + request_id + '"}]}}}}'
    )


def connect_signature(key: str, body: str) -> str:
    """Return the base64 HMAC-SHA256 DocuSign sends in X-DocuSign-Signature-N."""
    return base64.b64encode(hmac.new(key.encode(), body.encode(), hashlib.sha256).digest()).decode()


def signed(
    body: str, key: str = SENTINEL_HMAC_KEY, name: str = "X-DocuSign-Signature-1"
) -> list[tuple[str, str]]:
    """Return Connect request headers signing body with key."""
    return [("content-type", "application/json"), (name, connect_signature(key, body))]


async def load_project(
    directory: Path, bindings: Mapping[str, Any], hmac_key: str = SENTINEL_HMAC_KEY
) -> LoadedProject:
    """Write a Dex Web connections.json with one DocuSign connection and its bindings, then load it."""
    credentials = {
        "oauth_client_id": "inert-integration-key",
        "oauth_client_secret": "inert-client-secret",
        "access_token": "inert-access-token",
        "refresh_token": "inert-refresh-token",
    }
    if hmac_key:
        credentials["connect_hmac_key"] = hmac_key
    path = directory / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "docusign",
                        "connectionName": CONNECTION_NAME,
                        "modulePath": MODULE_PATH,
                        "provider": "docusign",
                        "authMethodId": "docusign-oauth",
                        "configuration": {"connectMaxBodyBytes": 4096},
                        "credentials": credentials,
                        # A fresh token: verifying Connect must not refresh it through account.docusign.com.
                        "credentialExpiresAt": "2099-01-01T00:00:00Z",
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "docusign",
                        "connectionName": CONNECTION_NAME,
                        "triggerName": "envelopeEventReceived",
                        "bindingName": name,
                        "configuration": configuration,
                    }
                    for name, configuration in bindings.items()
                ],
            }
        )
    )
    path.chmod(0o600)
    return await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})


class CollectingTarget:
    """Record every delivered event, optionally failing the first deliveries."""

    def __init__(self, failures: int = 0) -> None:
        self.events: asyncio.Queue[TriggerEvent[EnvelopeEvent]] = asyncio.Queue()
        self.failures = failures

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[EnvelopeEvent]) -> None:
        """Queue the event, or raise while failures remain so it stays pending."""
        if self.failures > 0:
            self.failures -= 1
            raise RuntimeError("target is temporarily unavailable")
        await self.events.put(event)

    async def next(self) -> TriggerEvent[EnvelopeEvent]:
        """Return the next delivered event, failing after five seconds."""
        return await asyncio.wait_for(self.events.get(), 5)
