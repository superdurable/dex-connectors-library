// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerDefinition, TriggerEvent } from '@superdurable/connectors-sdk';
import { z } from 'zod';
import { ConnectorID } from './generated/definitions.js';

/** Configuration of buttonClicked: the channel whose message buttons the binding receives, and optional filters. */
export const ButtonClickedTriggerConfigurationSchema = z.strictObject({
  /** Slack channel whose message buttons the binding receives. */ channelId: z.string(),
  /** Button action IDs, the MessageButton actionId values postButtonMessage posted; empty matches every button. */ actionIds: z.array(z.string()).nullable().optional(),
  /** Slack user IDs whose clicks match; empty matches a click by anyone who can see the message. */ clickerUserIds: z.array(z.string()).nullable().optional(),
}).superRefine((configuration, context) => {
  if (!configuration.channelId.trim()) context.addIssue({ code: 'custom', message: 'Slack channel ID is required' });
  if (!isUniqueNonEmpty(configuration.actionIds ?? [])) context.addIssue({ code: 'custom', message: 'Slack button action IDs must be non-empty and unique' });
  if (!isUniqueNonEmpty(configuration.clickerUserIds ?? [])) context.addIssue({ code: 'custom', message: 'Slack clicker user IDs must be non-empty and unique' });
});
/** Channel, action ID, and clicker filters of one buttonClicked binding. */
export type ButtonClickedTriggerConfiguration = z.infer<typeof ButtonClickedTriggerConfigurationSchema>;

/**
 * One click on a message button, decoded from a Slack block_actions payload; field names and order match the
 * Go connector. It carries IDs and the button's value, never the Slack response URL, trigger ID, or
 * verification token.
 */
export const ButtonClickEventSchema = z.object({
  /** Slack workspace ID of the clicked message. */ teamId: z.string(),
  /** Slack channel ID of the clicked message. */ channelId: z.string(),
  /** Timestamp of the message that holds the button. */ messageTimestamp: z.string(),
  /** Parent thread timestamp of that message, or messageTimestamp for a top-level message. */ threadTimestamp: z.string(),
  /** Slack user ID of the member who clicked. */ userId: z.string(),
  /** The clicked button's action ID. */ actionId: z.string(),
  /** The clicked button's value; absent when the button has none. */ value: z.string().optional(),
  /** Slack's timestamp of the click; each click has its own. */ actionTimestamp: z.string(),
});
/** One click on a message button. */
export type ButtonClickEvent = z.infer<typeof ButtonClickEventSchema>;

/** Manifest definition of buttonClicked. */
export const ButtonClickedTriggerDefinition: TriggerDefinition = {
  trigger: { connectorId: ConnectorID, triggerName: 'buttonClicked' },
  description: 'Receive a matching click on a message button that postButtonMessage posted, through Socket Mode.',
};

const nullableString = z.string().nullish().transform(value => value ?? '');
const idObject = z.object({ id: nullableString }).nullish().transform(value => value ?? { id: '' });
const BlockActionSchema = z.object({ type: nullableString, action_id: nullableString, value: nullableString, action_ts: nullableString }).nullable()
  .transform(value => value ?? { type: '', action_id: '', value: '', action_ts: '' });

const zeroBlockActionsPayload = { type: '', team: { id: '' }, user: { id: '' }, channel: { id: '' }, container: { type: '', message_ts: '', channel_id: '', thread_ts: '' }, message: { ts: '', thread_ts: '' }, actions: [] };

/** The block_actions fields the Trigger reads; Go's json.Unmarshal semantics, where null, including a null payload or action, is the zero value. */
export const BlockActionsPayloadSchema = z.object({
  type: nullableString, team: idObject, user: idObject, channel: idObject,
  container: z.object({ type: nullableString, message_ts: nullableString, channel_id: nullableString, thread_ts: nullableString }).nullish()
    .transform(value => value ?? { type: '', message_ts: '', channel_id: '', thread_ts: '' }),
  message: z.object({ ts: nullableString, thread_ts: nullableString }).nullish().transform(value => value ?? { ts: '', thread_ts: '' }),
  actions: z.array(BlockActionSchema).nullish().transform(value => value ?? []),
}).nullable().transform(value => value ?? zeroBlockActionsPayload);
/** One decoded block_actions payload. */
export type BlockActionsPayload = z.infer<typeof BlockActionsPayloadSchema>;

/** Return an event per accepted button action and a reason per ignored action or payload, with IDs and Slack enum values only. */
export function matchButtonClickPayload(configuration: ButtonClickedTriggerConfiguration, payload: BlockActionsPayload): { events: TriggerEvent<ButtonClickEvent>[]; ignored: string[] } {
  const channelId = blockActionsChannelId(payload);
  const messageTimestamp = payload.container.message_ts || payload.message.ts;
  const rejected = (reason: string) => ({ events: [], ignored: [reason] });
  if (payload.type !== 'block_actions') return rejected('not_block_actions');
  if (payload.container.type !== 'message') return rejected('not_a_message');
  if (channelId !== configuration.channelId) return rejected('channel_mismatch');
  if (!payload.user.id) return rejected('missing_user');
  if (!payload.team.id || !messageTimestamp) return rejected('missing_message_identity');
  if (!containsOrEmpty(configuration.clickerUserIds ?? [], payload.user.id)) return rejected('clicker_mismatch');
  const threadTimestamp = payload.container.thread_ts || payload.message.thread_ts || messageTimestamp;
  const events: TriggerEvent<ButtonClickEvent>[] = [];
  const ignored: string[] = [];
  for (const action of payload.actions) {
    if (action.type !== 'button') { ignored.push('not_a_button'); continue; }
    if (!action.action_id || !action.action_ts) { ignored.push('missing_action_identity'); continue; }
    if (!containsOrEmpty(configuration.actionIds ?? [], action.action_id)) { ignored.push('action_mismatch'); continue; }
    events.push({
      // Interactive payloads have no event ID. A click is unique by its message, button, and action time.
      id: [payload.team.id, channelId, messageTimestamp, action.action_id, action.action_ts].join(':'),
      occurredAt: slackTimestampTime(action.action_ts),
      payload: {
        teamId: payload.team.id, channelId, messageTimestamp, threadTimestamp, userId: payload.user.id, actionId: action.action_id,
        ...(action.value ? { value: action.value } : {}), actionTimestamp: action.action_ts,
      },
    });
  }
  if (payload.actions.length === 0) ignored.push('no_actions');
  return { events, ignored };
}

/** Return the clicked message's channel, from the container or the payload's channel. */
export function blockActionsChannelId(payload: BlockActionsPayload): string {
  return payload.container.channel_id || payload.channel.id;
}

/** Go's zero time, which the Go connector reports for an unparsable click timestamp. */
const zeroTime = new Date('0001-01-01T00:00:00Z');

/** Parse a Slack "seconds.micros" timestamp to the millisecond, returning Go's zero time when invalid. */
function slackTimestampTime(timestamp: string): Date {
  const seconds = Number(timestamp);
  if (!/^[0-9.eE+-]+$/.test(timestamp) || !Number.isFinite(seconds) || seconds <= 0) return zeroTime;
  const whole = Math.trunc(seconds);
  const microseconds = Math.round((seconds - whole) * 1e6);
  return new Date(whole * 1000 + Math.floor(microseconds / 1000));
}

function containsOrEmpty(allowed: readonly string[], value: string): boolean {
  return allowed.length === 0 || allowed.includes(value);
}

function isUniqueNonEmpty(values: readonly string[]): boolean {
  return values.every(value => value.trim() !== '') && new Set(values).size === values.length;
}
