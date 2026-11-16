// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { Client as DexClient } from '@superdurable/dex';
import { newDexFlowTriggerTarget } from '@superdurable/connectors-sdk';
import type { Runner } from '@superdurable/connectors-sdk/pollingtrigger';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectRecordChangedTriggerRunner, type AirtableClientOptions } from '../../src/index.js';
import { ConnectionName, RecordChangedTriggerBinding, acceptChangedRecord, mapToFlowInput, resolveFlowId, type RecordChangedIntakeFlow } from './flow/workflow.js';

/**
 * Route the saved binding's changes to the Flow through its durable project inbox and cursor. One runner seeds
 * the cursor, records each change in the binding's project inbox, and starts one Flow per change; it logs
 * every failed poll and delivery.
 */
export function newRecordChangedIntakeRunner(project: LoadedProject, client: DexClient, flow: RecordChangedIntakeFlow, options: AirtableClientOptions = {}): Runner {
  const target = newDexFlowTriggerTarget(client, flow, acceptChangedRecord, resolveFlowId, mapToFlowInput);
  return newProjectRecordChangedTriggerRunner(project, ConnectionName, [{ bindingName: RecordChangedTriggerBinding, target }], options);
}
