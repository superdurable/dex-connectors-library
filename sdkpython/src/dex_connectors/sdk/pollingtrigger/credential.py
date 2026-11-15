# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The stable credential Call identity of a polling Trigger."""

from uuid import NAMESPACE_URL, uuid5

from ..trigger import TriggerRef
from ..types import Call, CallID, ConnectionRef, OperationRef, new_trigger_call

_TRIGGER_CREDENTIAL_NAMESPACE = "https://superdurable.dev/dex-connectors/trigger-credential/v1"


def new_trigger_credential_call(connection: ConnectionRef, trigger: TriggerRef) -> Call:
    """Return the stable Call a poller uses to resolve and refresh connection credentials outside a Step.

    Its ID is the Go SDK's: a UUID v5 (URL namespace) over
    "https://superdurable.dev/dex-connectors/trigger-credential/v1/<provider>/<connection>/<trigger>", so
    every poll of one Trigger on one connection shares an identity across restarts, replicas, and
    languages, as the Gmail Trigger's calls do. The Call has no Dex context, so it cannot run a Connector
    Step. Raises ValueError for an invalid reference.
    """
    identity = "/".join(
        (_TRIGGER_CREDENTIAL_NAMESPACE, connection.provider, connection.name, trigger.trigger_name)
    )
    return new_trigger_call(
        connection,
        OperationRef(trigger.connector_id, trigger.trigger_name),
        CallID(str(uuid5(NAMESPACE_URL, identity))),
    )
