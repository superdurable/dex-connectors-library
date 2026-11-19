// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetCurrentWeatherStep, type Connection, type CurrentWeather, type GetCurrentWeatherInput } from '../../src/index.js';

/** Static Dex Web connection for the APISpace example. */
export const ConnectionName = 'apispace';
const weatherResult = new Attribute<CurrentWeather>('apispace-current-weather-result', jsonCodec<CurrentWeather>());

/** Read one city's current weather through APISpace. */
export class CurrentWeatherFlow implements Flow<GetCurrentWeatherInput> {
  private readonly start = new StartWeather();
  private readonly complete = new CompleteWeather();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('APISpace connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'APISpaceCurrentWeather'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetCurrentWeatherInput> {
    return StepList.startStep(this.start).otherSteps(newGetCurrentWeatherStep({
      stepType: 'CallAPISpaceWeather',
      annotations: { groupId: 'apispace', groupLabel: 'APISpace', explanation: 'Read the current observations for one city.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteAPISpaceWeather'),
    }), this.complete);
  }
  /** Register the durable weather observation. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [weatherResult] }; }
}

// dex:group group-id:apispace group-label:"APISpace"
// dex:explanation text:"Start the current-weather query."
class StartWeather implements Step<GetCurrentWeatherInput> {
  getStepType(): string { return 'StartAPISpaceWeather'; }
  execute(_context: Context, input: GetCurrentWeatherInput): StepDecision { return goTo(new StepRef('CallAPISpaceWeather'), input); }
}

// dex:group group-id:apispace group-label:"APISpace"
// dex:explanation text:"Persist the observation and complete the Flow."
class CompleteWeather implements Step<QueryResult<CurrentWeather>> {
  getStepType(): string { return 'CompleteAPISpaceWeather'; }
  execute(context: Context, result: QueryResult<CurrentWeather>): StepDecision { weatherResult.set(context, result.value); return gracefulComplete(result.value); }
}
