// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newListDevicesStep, type Connection, type DevicePage, type ListDevicesInput } from '../../src/index.js';

/** Static Dex Web connection for the Aqara example. */
export const ConnectionName = 'aqara';
const devicePage = new Attribute<DevicePage>('aqara-device-page', jsonCodec<DevicePage>());

/** Read one page of devices from an authorized Aqara account. */
export class DeviceListFlow implements Flow<ListDevicesInput> {
  private readonly start = new StartDeviceList();
  private readonly complete = new CompleteDeviceList();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('Aqara connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'AqaraDeviceList'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<ListDevicesInput> {
    return StepList.startStep(this.start).otherSteps(newListDevicesStep({
      stepType: 'CallAqaraDevices',
      annotations: { groupId: 'aqara', groupLabel: 'Aqara', explanation: 'Read one bounded device page for the authorized account.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, listed: new StepRef('CompleteAqaraDevices'),
    }), this.complete);
  }
  /** Register the durable device page. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [devicePage] }; }
}

// dex:group group-id:aqara group-label:"Aqara"
// dex:explanation text:"Start the authorized device query."
class StartDeviceList implements Step<ListDevicesInput> {
  getStepType(): string { return 'StartAqaraDevices'; }
  execute(_context: Context, input: ListDevicesInput): StepDecision { return goTo(new StepRef('CallAqaraDevices'), input); }
}

// dex:group group-id:aqara group-label:"Aqara"
// dex:explanation text:"Persist the device page and complete the Flow."
class CompleteDeviceList implements Step<QueryResult<DevicePage>> {
  getStepType(): string { return 'CompleteAqaraDevices'; }
  execute(context: Context, result: QueryResult<DevicePage>): StepDecision { devicePage.set(context, result.value); return gracefulComplete(result.value); }
}
