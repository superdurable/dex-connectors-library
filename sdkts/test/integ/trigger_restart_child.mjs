// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Records one acknowledged event in the local Dex Web inbox, then exits before any delivery succeeds,
// as a process that stops right after acknowledging a provider event does.
import process from 'node:process';
import { newTriggerContext, prepareTriggerDelivery } from '../../dist/index.js';
import { loadFromEnvironment, newDurableTriggerTarget } from '../../dist/projectconfig/index.js';

const [configurationFile, eventId, flowSuffix] = process.argv.slice(2);
const project = await loadFromEnvironment({ DEX_CONNECTOR_CONFIG_FILE: configurationFile });
const key = { connectorId: 'sample', connectionName: 'workspace', triggerName: 'itemCreated', bindingName: 'start' };
const quietLogger = { isEnabled: () => false, log: () => undefined };
const target = newDurableTriggerTarget(project.triggerInbox(key), key, { handleTrigger: async () => { throw new Error('Worker stopped'); } }, { logger: quietLogger });
const event = { id: eventId, occurredAt: new Date(), payload: { channel: 'C1', thread: `restart-${flowSuffix}` } };
await prepareTriggerDelivery(newTriggerContext(), target, event);
await target.handleTrigger(newTriggerContext(), event).catch(() => undefined);
process.exit(0);
