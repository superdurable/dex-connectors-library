// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGeocodeAddressStep, type Connection, type GeocodeAddressInput, type GeocodeAddressOutput } from '@superdurable/connector-amap';

/** Static Dex Web connection for the Amap example. */
export const ConnectionName = 'amap';
const addressLookupResult = new Attribute<GeocodeAddressOutput>('amap-address-lookup-result', jsonCodec<GeocodeAddressOutput>());

/** Geocode a structured Chinese address. */
export class AddressLookupFlow implements Flow<GeocodeAddressInput> {
  private readonly complete = new CompleteAddressLookup();
  private readonly call;

  /** Bind the static Amap connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Amap connection name differs');
    this.call = newGeocodeAddressStep<GeocodeAddressInput>({
      stepType: 'CallAmap',
      annotations: { groupId: 'amap', groupLabel: 'Amap', explanation: 'Resolve the supplied address to Amap location candidates.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, geocoded: new StepRef('CompleteAmapAddressLookup'),
    });
  }

  /** Preserve the Flow identity. */
  getFlowType(): string { return 'AmapAddressLookup'; }

  /** Register provider and terminal Steps. */
  getSteps(): StepList<GeocodeAddressInput> {
    return StepList.startStep(this.call).otherSteps(this.complete);
  }

  /** Register the durable address result. */
  getPersistenceSchema(): PersistenceSchema { return { attributes: [addressLookupResult] }; }
}

// dex:group group-id:amap group-label:"Amap"
// dex:explanation text:"Persist the Amap locations and complete the Flow."
class CompleteAddressLookup implements Step<QueryResult<GeocodeAddressOutput>> {
  getStepType(): string { return 'CompleteAmapAddressLookup'; }
  execute(context: Context, result: QueryResult<GeocodeAddressOutput>): StepDecision {
    addressLookupResult.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
