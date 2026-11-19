// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetCurrentWeatherStep, type Connection, type GetCurrentWeatherInput, type GetCurrentWeatherOutput } from '@superdurable/connector-geovisearth';

/** Static Dex Web connection for this example. */
export const ConnectionName = 'geovisearth';
const resultAttribute = new Attribute<GetCurrentWeatherOutput>('geovisearth-result', jsonCodec<GetCurrentWeatherOutput>());

/** Query Geovisearth through a typed connector Step. */
export class GeovisearthInspectionFlow implements Flow<GetCurrentWeatherInput> {
  private readonly complete = new CompleteInspection();
  private readonly read;

  /** Bind one named provider connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('Geovisearth connection name differs');
    this.read = newGetCurrentWeatherStep<GetCurrentWeatherInput>({
      stepType: 'CallGetCurrentWeather',
      annotations: { groupId: 'provider', groupLabel: 'Geovisearth', explanation: 'Read provider data through Geovisearth.' },
      connection, connectionName: ConnectionName,
      mapToOperationInput: input => input,
      fetched: new StepRef('CompleteGeovisearth'),
    });
  }

  /** Return the stable Flow identity. */ getFlowType(): string { return 'GeovisearthInspectionTypeScript'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetCurrentWeatherInput> { return StepList.startStep(this.read).otherSteps(this.complete); }
  /** Persist the selected provider result. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [resultAttribute] }; }
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the selected provider result and complete the Flow."
class CompleteInspection implements Step<QueryResult<GetCurrentWeatherOutput>> {
  /** Return the stable Step identity. */ getStepType(): string { return 'CompleteGeovisearth'; }
  /** Persist the Query result. */
  execute(context: Context, result: QueryResult<GetCurrentWeatherOutput>): StepDecision {
    resultAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
