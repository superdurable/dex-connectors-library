// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { TriggerBindingRef, TriggerDefinition, TriggerSource } from '../trigger.js';
import type { TriggerContext, TriggerEvent, TriggerLogger, TriggerTarget } from '../trigger_delivery.js';
import type { CursorStore } from './cursor_store.js';
import type { ChangeFetcher } from './fetcher.js';
import { PollMember, bindingLogAttributes, deliveryModeFor, validateMemberBinding, type FeedMember, type MemberDeliveryMode } from './member.js';
import { CursorPoller, type Poller } from './poller.js';
import { newRunner, resolvePollSchedule, type PollSchedule } from './runner.js';
import { resolveKeyedSnapshotOptions, type KeyedSnapshotOptions, type ResolvedKeyedSnapshotOptions } from './snapshot.js';

/** A stored feed name follows the manifest Trigger name format. */
const feedNamePattern = /^[a-z][A-Za-z0-9]{1,62}$/;

/** Configures a poller that owns one Trigger binding and its cursor. */
export interface BindingPollerConfig<Payload> {
  /** The immutable Trigger definition; binding.trigger must equal definition.trigger. */ definition: TriggerDefinition;
  /** The configured Trigger binding. Its name is the poller name for wakePoller. */ binding: TriggerBindingRef;
  /** Persists the binding's position, normally LoadedProject.pollCursorStore. */ cursor: CursorStore;
  /** Reads the provider; its changes are this binding's events. */ fetcher: ChangeFetcher<TriggerEvent<Payload>>;
  /**
   * Receives events. Wrap it with newDurableTriggerTarget so events are recorded in the project Trigger inbox
   * before the cursor commits; a non-durable target consumes each event first. newBindingTriggerSource
   * requires it to be omitted.
   */
  target?: TriggerTarget<Payload> | undefined;
  /** Enables a keyed snapshot that the fetcher keeps in Cursor.snapshot. Omitted disables it. */ snapshot?: KeyedSnapshotOptions | undefined;
}

/**
 * Validate config and return a Poller for newRunner. It reads and writes nothing; throws for invalid wiring.
 */
export function newBindingPoller<Payload>(config: BindingPollerConfig<Payload>): Poller {
  if (!config.target) throw new TypeError('poll Trigger target is required');
  return buildBindingPoller(config, config.target, deliveryModeFor(config.target), true);
}

function buildBindingPoller<Payload>(config: BindingPollerConfig<Payload>, target: TriggerTarget<Payload>, mode: MemberDeliveryMode, shouldReplay: boolean): CursorPoller<TriggerEvent<Payload>> {
  const binding = validateMemberBinding(config.definition, config.binding);
  if (!config.cursor || !config.fetcher) throw new TypeError('poll Trigger cursor store and fetcher are required');
  const snapshot = resolvePollerSnapshot(config.snapshot);
  const member = new PollMember<TriggerEvent<Payload>, Payload>(binding, change => [change], target, mode, shouldReplay);
  return new CursorPoller(binding.name, bindingLogAttributes(binding), config.cursor, config.fetcher, [member], snapshot);
}

/**
 * Configures one provider change feed shared by several Trigger bindings of one connection, so the provider
 * is read once per poll for all of them.
 */
export interface FeedPollerConfig<Change> {
  /**
   * The feed's storage identity (PollFeedKey.feedName) and its poller name for wakePoller: lower camel case,
   * 2 to 63 characters, and stable forever.
   */
  name: string;
  /** Persists the feed position, normally LoadedProject.pollFeedCursorStore. */ cursor: CursorStore;
  /** Reads the provider feed once per poll for every member. */ fetcher: ChangeFetcher<Change>;
  /** Recorded and delivered in this order for every page; all share one connector and connection, at least one. */ members: FeedMember<Change>[];
  /**
   * Enables a keyed snapshot that the fetcher keeps in Cursor.snapshot. When a change implements KeyedItem,
   * each selector receives the change's entry from the committed snapshot.
   */
  snapshot?: KeyedSnapshotOptions | undefined;
}

/** Validate config and return a Poller for newRunner. It reads and writes nothing; throws for invalid wiring. */
export function newFeedPoller<Change>(config: FeedPollerConfig<Change>): Poller {
  if (!feedNamePattern.test(config.name)) throw new TypeError('poll feed name must be lower camel case, 2 to 63 characters');
  if (!config.cursor || !config.fetcher || !config.members?.length) throw new TypeError('poll feed cursor store, fetcher, and members are required');
  const snapshot = resolvePollerSnapshot(config.snapshot);
  const members: PollMember<Change, unknown>[] = [];
  for (const configured of config.members) {
    if (!(configured instanceof PollMember)) throw new TypeError('poll feed member must come from newFeedMember');
    const first = members[0]?.binding ?? configured.binding;
    if (configured.binding.trigger.connectorId !== first.trigger.connectorId || configured.binding.connection.provider !== first.connection.provider || configured.binding.connection.name !== first.connection.name) {
      throw new TypeError(`poll feed "${config.name}" members must share one connector and connection`);
    }
    members.push(configured as PollMember<Change, unknown>);
  }
  const first = members[0]!.binding;
  return new CursorPoller(config.name, { connector: first.trigger.connectorId, connection: first.connection.name, feed: config.name }, config.cursor, config.fetcher, members, snapshot);
}

function resolvePollerSnapshot(options: KeyedSnapshotOptions | undefined): ResolvedKeyedSnapshotOptions | undefined {
  return options === undefined ? undefined : resolveKeyedSnapshotOptions(options);
}

/**
 * Run a single-binding poll loop as a TriggerSource, so a per-Trigger factory keeps using newTrigger. The
 * target newTrigger passes to run replaces config.target, which must be omitted. Because that target hides
 * whether events are durable, the source records each event with prepareTriggerDelivery and also consumes it
 * before the cursor commits. newTrigger replays a durable target before run, so the source does not replay it
 * again. logger receives the runner's records. Throws for invalid wiring or an invalid schedule.
 */
export function newBindingTriggerSource<Payload>(config: BindingPollerConfig<Payload>, schedule: PollSchedule, logger?: TriggerLogger): TriggerSource<Payload> {
  if (config.target) throw new TypeError('poll Trigger source target must be omitted; newTrigger supplies it');
  buildBindingPoller(config, { handleTrigger: async () => { throw new Error('poll Trigger source target is not configured'); } }, 'recordAndDeliverBeforeCommit', false);
  resolvePollSchedule(schedule);
  return {
    async run(context: TriggerContext, target: TriggerTarget<Payload>): Promise<void> {
      if (!target) throw new TypeError('poll Trigger source target is required');
      const poller = buildBindingPoller(config, target, 'recordAndDeliverBeforeCommit', false);
      await newRunner({ connection: config.binding.connection, schedule, pollers: [poller], logger }).run(context);
    },
  };
}
