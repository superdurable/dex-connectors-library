// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newLookupIPLocationStep, type Connection, type LookupIPLocationInput, type LookupIPLocationOutput } from '../../src/index.js';

/** Static Dex Web connection for the ALAPI example. */
export const ConnectionName = 'alapi';
const locationResult = new Attribute<LookupIPLocationOutput>('alapi-location-result', jsonCodec<LookupIPLocationOutput>());

/** Read one public IPv4 address through ALAPI. */
export class IPLocationFlow implements Flow<LookupIPLocationInput> {
  private readonly start = new StartLookup();
  private readonly complete = new CompleteLookup();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('ALAPI connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'ALAPIIPLocation'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<LookupIPLocationInput> {
    return StepList.startStep(this.start).otherSteps(newLookupIPLocationStep({
      stepType: 'CallALAPI',
      annotations: { groupId: 'alapi', groupLabel: 'ALAPI', explanation: 'Read coarse location fields for one public IPv4 address.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteALAPILookup'),
    }), this.complete);
  }
  /** Register the durable location result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [locationResult] }; }
}

// dex:group group-id:alapi group-label:"ALAPI"
// dex:explanation text:"Start the ALAPI IP location request."
class StartLookup implements Step<LookupIPLocationInput> {
  getStepType(): string { return 'StartALAPILookup'; }
  execute(_context: Context, input: LookupIPLocationInput): StepDecision { return goTo(new StepRef('CallALAPI'), input); }
}

// dex:group group-id:alapi group-label:"ALAPI"
// dex:explanation text:"Persist the location and complete the Flow."
class CompleteLookup implements Step<QueryResult<LookupIPLocationOutput>> {
  getStepType(): string { return 'CompleteALAPILookup'; }
  execute(context: Context, result: QueryResult<LookupIPLocationOutput>): StepDecision { locationResult.set(context, result.value); return gracefulComplete(result.value); }
}
