// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';
import { createTriggerCall, type Call } from '../credentials.js';
import type { TriggerRef } from '../trigger.js';
import type { ConnectionRef } from '../types.js';

/** Prefix of the name-based identity of a Trigger's credential calls. */
const triggerCredentialNamespace = 'https://superdurable.dev/dex-connectors/trigger-credential/v1';
/** RFC 4122 URL namespace, as Go's uuid.NameSpaceURL. */
const urlNamespace = '6ba7b8119dad11d180b400c04fd430c8';

/**
 * Return the stable credential Call a poller uses to resolve and refresh connection credentials outside a
 * Dex Step. Its ID is a UUID v5 (URL namespace) over
 * "https://superdurable.dev/dex-connectors/trigger-credential/v1/<provider>/<connection>/<trigger>", so every
 * poll of one Trigger on one connection shares an identity across restarts, replicas, and the Go SDK, as the
 * Gmail Trigger's calls do. The call carries only signal as its context, so it cannot run a Connector Step.
 */
export function newTriggerCredentialCall(connection: ConnectionRef, trigger: TriggerRef, signal: AbortSignal): Call {
  return createTriggerCall(connection, { connectorId: trigger.connectorId, operationId: trigger.triggerName }, triggerCredentialCallId(connection, trigger), signal);
}

/** Return the ID newTriggerCredentialCall gives the credential calls of trigger on connection. */
export function triggerCredentialCallId(connection: ConnectionRef, trigger: TriggerRef): string {
  const identity = [triggerCredentialNamespace, connection.provider, connection.name, trigger.triggerName].join('/');
  const digest = createHash('sha1').update(Buffer.from(urlNamespace, 'hex')).update(identity, 'utf8').digest();
  digest[6] = (digest[6]! & 0x0f) | 0x50;
  digest[8] = (digest[8]! & 0x3f) | 0x80;
  const hex = digest.subarray(0, 16).toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
