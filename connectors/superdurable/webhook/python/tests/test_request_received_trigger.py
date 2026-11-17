# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cover inbound webhook authentication, IDs, filters, and secret-safe forwarding."""

import asyncio
import base64
import hashlib
import hmac
import json
from datetime import UTC, datetime
from pathlib import Path

import pytest
from dex_connectors.sdk import TriggerCancellation, TriggerContext, TriggerDeliveryAbortedError, TriggerEvent
from dex_connectors.sdk.projectconfig import load_from_environment

from dex_connectors.superdurable_webhook import (
    ProjectRequestReceivedTriggerRoute,
    new_project_connection,
    new_project_request_received_webhook_runtime,
)

SECRET = "inert-shared-secret"


class Collector:
    """Collect accepted events."""

    def __init__(self) -> None:
        self.events: asyncio.Queue[TriggerEvent[object]] = asyncio.Queue()

    async def handle_trigger(self, context: TriggerContext, event: TriggerEvent[object]) -> None:
        """Receive a decoded event."""
        await self.events.put(event)


async def exercise(
    tmp_path: Path,
    config: dict[str, object],
    binding: dict[str, object],
    payload: bytes,
    headers: list[tuple[str, str]],
    expected_status: int = 200,
    secret: str = SECRET,
) -> TriggerEvent[object] | None:
    """Run a real durable endpoint against inert project configuration."""
    path = tmp_path / "connections.json"
    path.write_text(
        json.dumps(
            {
                "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
                "connections": [
                    {
                        "connectorId": "webhook",
                        "connectionName": "forms",
                        "provider": "webhook",
                        "modulePath": (
                            "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
                        ),
                        "authMethodId": "webhook-signing-secret",
                        "configuration": config,
                        "credentials": {"signing_secret": secret},
                    }
                ],
                "triggerBindings": [
                    {
                        "connectorId": "webhook",
                        "connectionName": "forms",
                        "triggerName": "requestReceived",
                        "bindingName": "submissions",
                        "configuration": binding,
                    }
                ],
            }
        )
    )
    path.chmod(0o600)
    project = await load_from_environment({"DEX_CONNECTOR_CONFIG_FILE": str(path)})
    connection = new_project_connection(project, "forms")
    collector = Collector()
    runtime = new_project_request_received_webhook_runtime(
        project, connection, [ProjectRequestReceivedTriggerRoute("submissions", collector)]
    )
    cancellation = TriggerCancellation()
    task = asyncio.create_task(runtime.run(TriggerContext(cancellation)))
    try:

        async def ready() -> None:
            while runtime.running_source_count() != 1:
                if task.done():
                    await task
                await asyncio.sleep(0.01)

        await asyncio.wait_for(ready(), 5)
        response = await runtime.handle_request("POST", "/hooks/forms", headers, payload)
        assert response.status == expected_status
        if expected_status == 200 and not collector.events.empty():
            return collector.events.get_nowait()
        await asyncio.sleep(0)
        return collector.events.get_nowait() if not collector.events.empty() else None
    finally:
        cancellation.cancel()
        with pytest.raises(TriggerDeliveryAbortedError):
            await task
        await connection.aclose()
        await project.close()


@pytest.mark.asyncio
async def test_hmac_json_id_priority_filter_and_sensitive_headers(tmp_path: Path) -> None:
    """HMAC uses raw bytes; a sender ID wins and credentials cannot be forwarded."""
    payload = b'{"event_id":"evt_body","kind":"form.submitted","count":42}'
    signature = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
    event = await exercise(
        tmp_path,
        {
            "eventIdHeader": "X-Delivery",
            "eventIdPointer": "/event_id",
            "forwardedHeaders": ["X-Event", "X-Event"],
        },
        {"matchPointer": "/kind", "matchValues": ["form.submitted"]},
        payload,
        [
            ("Content-Type", "application/json; charset=utf-8"),
            ("X-Signature-256", signature),
            ("X-Delivery", "delivery_123"),
            ("X-Event", "form"),
            ("Authorization", "Bearer secret"),
        ],
    )
    assert event is not None and event.id == "delivery_123"
    assert event.payload.headers == {"X-Event": "form"}
    assert event.payload.content_type == "application/json"
    assert "Bearer secret" not in repr(event)


@pytest.mark.asyncio
async def test_hmac_rejection_and_form_pointer(tmp_path: Path) -> None:
    """Bad signatures fail and form values can supply a stable event ID."""
    payload = b"id=sub-7&kind=form.submitted&kind=duplicate"
    signature = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
    assert (
        await exercise(
            tmp_path,
            {"eventIdPointer": "/id/0"},
            {},
            payload,
            [("Content-Type", "application/x-www-form-urlencoded"), ("X-Signature-256", "0" * 64)],
            400,
        )
        is None
    )
    event = await exercise(
        tmp_path,
        {"eventIdPointer": "/id/0"},
        {},
        payload,
        [("Content-Type", "application/x-www-form-urlencoded"), ("X-Signature-256", signature)],
    )
    assert event is not None and event.id == "sub-7"
    assert event.payload.form_body["kind"] == ("form.submitted", "duplicate")


@pytest.mark.asyncio
async def test_standard_webhooks_timestamp_and_shared_token(tmp_path: Path) -> None:
    """Standard Webhooks rejects replay; shared token uses the configured header."""
    payload = b'{"kind":"form.submitted"}'
    timestamp = str(int(datetime.now(UTC).timestamp()))
    event_id = "msg_123"
    signed = event_id.encode() + b"." + timestamp.encode() + b"." + payload
    signature = base64.b64encode(hmac.new(SECRET.encode(), signed, hashlib.sha256).digest()).decode()
    headers = [
        ("Content-Type", "application/json"),
        ("webhook-id", event_id),
        ("webhook-timestamp", timestamp),
        ("webhook-signature", "v1," + signature),
    ]
    event = await exercise(tmp_path, {"verification": "standardWebhooks"}, {}, payload, headers)
    assert event is not None and event.id == event_id
    stale = [(key, "1000000000" if key == "webhook-timestamp" else value) for key, value in headers]
    assert await exercise(tmp_path, {"verification": "standardWebhooks"}, {}, payload, stale, 400) is None
    event = await exercise(
        tmp_path,
        {"verification": "sharedToken", "tokenHeader": "X-Form-Token"},
        {},
        payload,
        [("Content-Type", "application/json"), ("X-Form-Token", SECRET)],
    )
    assert event is not None and event.id == hashlib.sha256(payload).hexdigest()


def test_configuration_guidance_and_secret_safe_forwarding() -> None:
    """Every visible receive field has setup guidance and secret headers are denied."""
    from dataclasses import replace

    import yaml
    from dex_connectors.sdk import SecretString

    from dex_connectors.superdurable_webhook import Client, Config, Credentials

    manifest = yaml.safe_load((Path(__file__).parents[2] / "connector.yaml").read_text())
    authorization = manifest["spec"]["auth"]
    assert authorization["guide"]["startURL"].startswith("https://")
    assert authorization["guide"]["steps"]
    fields = manifest["spec"]["configuration"]["fields"]
    for field in fields:
        assert field["description"] and len(field["description"]) > 40
        assert "Blank" in field["description"] or "blank" in field["description"]
    config = Config("", "", "", (), "")

    class CredentialsProvider:
        async def resolve(self, call: object) -> Credentials:
            return Credentials(SecretString(SECRET))

    with pytest.raises(ValueError, match="forwardedHeaders"):
        Client(replace(config, forwarded_headers=("Authorization",)), CredentialsProvider())
    with pytest.raises(ValueError, match="forwardedHeaders"):
        Client(replace(config, forwarded_headers=("Webhook-Signature",)), CredentialsProvider())


@pytest.mark.asyncio
async def test_numeric_filter_and_unmatched_acknowledgment(tmp_path: Path) -> None:
    """A numeric JSON scalar matches its literal; rejected bindings still answer 200."""
    payload = b'{"event_id":"evt_numeric","count":42}'
    signature = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
    headers = [("Content-Type", "application/json"), ("X-Signature-256", signature)]
    event = await exercise(
        tmp_path,
        {"eventIdPointer": "/event_id"},
        {"matchPointer": "/count", "matchValues": ["42"]},
        payload,
        headers,
    )
    assert event is not None and event.id == "evt_numeric"
    assert (
        await exercise(
            tmp_path,
            {"eventIdPointer": "/event_id"},
            {"matchPointer": "/count", "matchValues": ["43"]},
            payload,
            headers,
        )
        is None
    )


@pytest.mark.asyncio
async def test_hmac_base64_prefix_and_invalid_event_id(tmp_path: Path) -> None:
    """The configured signature format is exact and invalid sender IDs answer 400."""
    payload = b'{"id":"evt_123"}'
    signature = base64.b64encode(hmac.new(SECRET.encode(), payload, hashlib.sha256).digest()).decode()
    config: dict[str, object] = {
        "signatureHeader": "Typeform-Signature",
        "signaturePrefix": "sha256=",
        "signatureEncoding": "base64",
        "eventIdHeader": "X-Delivery",
    }
    headers = [
        ("Content-Type", "application/json"),
        ("Typeform-Signature", "sha256=" + signature),
        ("X-Delivery", "evt_123"),
    ]
    event = await exercise(tmp_path, config, {}, payload, headers)
    assert event is not None and event.id == "evt_123"
    invalid = [(name, "bad id" if name == "X-Delivery" else value) for name, value in headers]
    assert await exercise(tmp_path, config, {}, payload, invalid, 400) is None


@pytest.mark.asyncio
async def test_standard_webhooks_prefixed_secret(tmp_path: Path) -> None:
    """A whsec_ value is decoded before signing, as Standard Webhooks defines."""
    payload = b'{"kind":"form.submitted"}'
    key = b"\x01" * 32
    secret = "whsec_" + base64.b64encode(key).decode()
    timestamp = str(int(datetime.now(UTC).timestamp()))
    event_id = "msg_prefixed"
    signature = base64.b64encode(
        hmac.new(key, event_id.encode() + b"." + timestamp.encode() + b"." + payload, hashlib.sha256).digest()
    ).decode()
    event = await exercise(
        tmp_path,
        {"verification": "standardWebhooks"},
        {},
        payload,
        [
            ("Content-Type", "application/json"),
            ("webhook-id", event_id),
            ("webhook-timestamp", timestamp),
            ("webhook-signature", "v0,ignored v1," + signature),
        ],
        secret=secret,
    )
    assert event is not None and event.id == event_id


@pytest.mark.asyncio
async def test_numeric_filter_preserves_json_number_spelling(tmp_path: Path) -> None:
    """A number filter matches the literal token as Go json.Number does."""
    payload = b'{"event_id":"evt_exponent","count":1e3}'
    signature = hmac.new(SECRET.encode(), payload, hashlib.sha256).hexdigest()
    event = await exercise(
        tmp_path,
        {"eventIdPointer": "/event_id"},
        {"matchPointer": "/count", "matchValues": ["1e3"]},
        payload,
        [("Content-Type", "application/json"), ("X-Signature-256", signature)],
    )
    assert event is not None and event.id == "evt_exponent"
