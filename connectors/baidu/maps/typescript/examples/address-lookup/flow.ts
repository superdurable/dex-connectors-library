// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGeocodeAddressStep, type Connection, type GeocodeAddressInput, type GeocodeAddressOutput } from '@superdurable/connector-baidu-maps';

/** Static Dex Web connection name. */
export const ConnectionName = 'baidu-maps';
const locationAttribute = new Attribute('baidu-maps-address-lookup-location', jsonCodec<GeocodeAddressOutput>());

/** Resolve one Chinese address into a persisted BD-09 coordinate. */
export class BaiduMapsAddressLookupFlow implements Flow<GeocodeAddressInput> {
  private readonly completed = new AddressGeocoded();
  private readonly lookup;
  /** Bind the named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('address lookup requires the baidu-maps connection');
    this.lookup = newGeocodeAddressStep<GeocodeAddressInput>({
      stepType: 'GeocodeBaiduAddress',
      annotations: { groupId: 'baidu-maps', groupLabel: 'Baidu Maps', explanation: 'Resolve the Chinese street address through Baidu Maps Geocoding v3.' },
      connection, connectionName: ConnectionName, mapToOperationInput: request => request,
      geocoded: new StepRef('AddressGeocoded'),
    });
  }
  /** Stable Dex Flow identity. */ getFlowType(): string { return 'BaiduMapsAddressLookup'; }
  /** Register provider and completion Steps. */
  getSteps(): StepList<GeocodeAddressInput> { return StepList.startStep<GeocodeAddressInput>(this.lookup).otherSteps(this.completed); }
  /** Register the persisted coordinate. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [locationAttribute] }; }
}

// dex:group group-id:baidu-maps group-label:"Baidu Maps"
// dex:explanation text:"Persist the successful BD-09 coordinate and complete the Flow."
class AddressGeocoded implements Step<QueryResult<GeocodeAddressOutput>> {
  getStepType(): string { return 'AddressGeocoded'; }
  execute(context: Context, result: QueryResult<GeocodeAddressOutput>): StepDecision {
    locationAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
