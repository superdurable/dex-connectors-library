// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetCurrentWeatherStep, type Connection, type GetCurrentWeatherInput, type GetCurrentWeatherOutput } from '@superdurable/connector-caiyun';

/** Static Caiyun connection name used in Dex Web. */
export const ConnectionName = 'caiyun';
/** Durable current weather values without credentials. */
export const currentWeatherAttribute = new Attribute('caiyun-current-weather', jsonCodec<GetCurrentWeatherOutput>());

/** Read one realtime observation and persist metric values. */
export class CaiyunCurrentWeatherFlow implements Flow<GetCurrentWeatherInput> {
  private readonly complete = new CompleteCurrentWeather();
  private readonly query;
  /** Bind the named Caiyun connection. */
  constructor(connection: Connection) {
    if (connection.reference.name !== ConnectionName) throw new TypeError('current weather Flow requires the caiyun connection');
    this.query = newGetCurrentWeatherStep<GetCurrentWeatherInput>({
      stepType: 'GetCaiyunCurrentWeather',
      annotations: { groupId: 'caiyun', groupLabel: 'Caiyun', explanation: 'Read realtime weather for the supplied coordinate.' },
      connection, connectionName: ConnectionName, mapToOperationInput: input => input,
      fetched: new StepRef('CompleteCaiyunCurrentWeather'),
    });
  }
  /** Preserve the stable Flow identity. */ getFlowType(): string { return 'CaiyunCurrentWeather'; }
  /** Register provider and completion Steps. */ getSteps(): StepList<GetCurrentWeatherInput> { return StepList.startStep(this.query).otherSteps(this.complete); }
  /** Register the durable weather values. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [currentWeatherAttribute] }; }
}

// dex:group group-id:caiyun group-label:"Caiyun"
// dex:explanation text:"Persist the realtime weather values and complete the Flow."
class CompleteCurrentWeather implements Step<QueryResult<GetCurrentWeatherOutput>> {
  getStepType(): string { return 'CompleteCaiyunCurrentWeather'; }
  execute(context: Context, result: QueryResult<GetCurrentWeatherOutput>): StepDecision {
    currentWeatherAttribute.set(context, result.value);
    return gracefulComplete(result.value);
  }
}
