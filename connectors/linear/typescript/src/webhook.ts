// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHmac, timingSafeEqual } from 'node:crypto';
import type { TriggerEvent } from '@superdurable/connectors-sdk';
import { WebhookVerificationUnavailableError, type WebhookRequest } from '@superdurable/connectors-sdk/webhooktrigger';
import { IssueSummarySchema, type Credentials } from './generated/models.js';
import { IssueEventSchema, type IssueEvent } from './trigger.js';

const uuidPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const tokenPattern = /^[A-Za-z0-9_.:-]{1,64}$/;

/** Verify Linear's raw-body HMAC before trusting its webhookTimestamp. */
export function verifyLinearWebhookRequest(request: WebhookRequest, credentials: Credentials, toleranceMs: number): void {
  const secret = credentials.webhookSigningSecret?.reveal() ?? '';
  if (!secret) throw new WebhookVerificationUnavailableError();
  const value = request.headers['linear-signature'];
  const signature = (Array.isArray(value) ? value[0] ?? '' : value ?? '').trim();
  if (!/^[0-9a-fA-F]{64}$/.test(signature)) throw new TypeError('Linear-Signature is invalid');
  const actual = Buffer.from(signature, 'hex');
  const expected = createHmac('sha256', secret).update(request.body).digest();
  if (!timingSafeEqual(actual, expected)) throw new TypeError('Linear-Signature is invalid');
  const envelope: unknown = JSON.parse(Buffer.from(request.body).toString('utf8'));
  if (!envelope || typeof envelope !== 'object' || Array.isArray(envelope)) throw new TypeError('Linear webhook timestamp is invalid');
  const timestamp = (envelope as Record<string, unknown>).webhookTimestamp;
  if (typeof timestamp !== 'number' || !Number.isFinite(timestamp) || timestamp <= 0 || timestamp > Number.MAX_SAFE_INTEGER / 2 ||
      Math.abs(request.receivedAt.getTime() - Math.trunc(timestamp)) > toleranceMs)
    throw new TypeError('Linear webhook timestamp is invalid');
}

/** Decode a verified Issue delivery, omitting sensitive descriptions and old values. */
export function decodeIssueWebhookRequest(request: WebhookRequest): TriggerEvent<IssueEvent> | undefined {
  const envelope: unknown = JSON.parse(Buffer.from(request.body).toString('utf8'));
  if (!envelope || typeof envelope !== 'object' || Array.isArray(envelope)) throw new TypeError('Linear webhook body is malformed');
  const fields = envelope as Record<string, unknown>;
  if (typeof fields.type !== 'string' || typeof fields.action !== 'string' || !tokenPattern.test(fields.action)) throw new TypeError('Linear webhook body is malformed');
  if (fields.type !== 'Issue') return undefined;
  if (!fields.data || typeof fields.data !== 'object' || Array.isArray(fields.data)) throw new TypeError('Linear webhook body is malformed');
  const wire = fields.data as Record<string, unknown>;
  const team = wire.team ?? (uuidPattern.test(String(wire.teamId ?? '')) ? { id: wire.teamId } : undefined);
  const state = wire.state ?? (uuidPattern.test(String(wire.stateId ?? '')) ? { id: wire.stateId } : undefined);
  const assignee = wire.assignee ?? (typeof wire.assigneeId === 'string' ? { id: wire.assigneeId } : undefined);
  const teamFields = team && typeof team === 'object' && !Array.isArray(team) ? team as Record<string, unknown> : {};
  const stateFields = state && typeof state === 'object' && !Array.isArray(state) ? state as Record<string, unknown> : {};
  const summary = IssueSummarySchema.parse({
    id: wire.id, identifier: wire.identifier, title: wire.title ?? '', url: wire.url ?? '',
    team: { id: teamFields.id, key: teamFields.key ?? '', name: teamFields.name ?? '' },
    state: { id: stateFields.id, name: stateFields.name ?? '', type: stateFields.type ?? '' },
    priority: wire.priority ?? 0, createdAt: wire.createdAt, updatedAt: wire.updatedAt,
    ...(assignee ? { assigneeId: (assignee as Record<string, unknown>).id } : {}),
    ...(wire.labelIds ? { labelIds: wire.labelIds } : {}),
    ...(wire.dueDate ? { dueDate: wire.dueDate } : {}),
    ...(wire.archivedAt ? { archivedAt: wire.archivedAt } : {}),
  });
  if (!uuidPattern.test(summary.id) || !uuidPattern.test(summary.team.id) || !uuidPattern.test(summary.state.id) || !summary.identifier.trim() ||
      summary.priority < 0 || summary.priority > 4 || !Number.isInteger(wire.number) || (wire.number as number) < 0 || (wire.number as number) > 2_147_483_647)
    throw new TypeError('Linear webhook issue is malformed');
  const occurredAt = parseTime(fields.createdAt);
  const changes = fields.updatedFrom && typeof fields.updatedFrom === 'object' && !Array.isArray(fields.updatedFrom)
    ? fields.updatedFrom as Record<string, unknown> : {};
  const updatedFields = Object.keys(changes).filter(field => tokenPattern.test(field)).sort().slice(0, 50);
  const actorWire = fields.actor;
  const actor = actorWire && typeof actorWire === 'object' && !Array.isArray(actorWire) ? actorWire as Record<string, unknown> : undefined;
  const payload: IssueEvent = IssueEventSchema.parse({
    action: fields.action, occurredAt, webhookId: typeof fields.webhookId === 'string' ? fields.webhookId : '',
    organizationId: typeof fields.organizationId === 'string' ? fields.organizationId : '',
    ...(actor ? { actor: { id: actor.id, ...(typeof actor.name === 'string' && actor.name ? { name: actor.name } : {}), ...(typeof actor.type === 'string' && actor.type ? { type: actor.type } : {}) } } : {}),
    issue: { ...summary, number: wire.number, ...(typeof wire.projectId === 'string' ? { projectId: wire.projectId } : {}),
      ...(typeof wire.cycleId === 'string' ? { cycleId: wire.cycleId } : {}), ...(typeof wire.parentId === 'string' ? { parentId: wire.parentId } : {}),
      ...(wire.trashed === true ? { trashed: true } : {}) },
    ...(updatedFields.length ? { updatedFields } : {}),
    ...(typeof changes.stateId === 'string' && uuidPattern.test(changes.stateId) ? { previousStateId: changes.stateId } : {}),
  });
  return { id: `${payload.action}:${summary.id}:${Date.parse(summary.updatedAt)}`, occurredAt: new Date(occurredAt), payload };
}
function parseTime(value: unknown): string {
  if (typeof value !== 'string' || !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value)) throw new TypeError('Linear webhook time is malformed');
  const parsed = new Date(value);
  if (!Number.isFinite(parsed.getTime())) throw new TypeError('Linear webhook time is malformed');
  return parsed.toISOString().replace('.000Z', 'Z');
}
