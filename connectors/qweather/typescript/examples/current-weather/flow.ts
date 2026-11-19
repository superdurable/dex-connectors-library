// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { Attribute, StepList, StepRef, goTo, gracefulComplete, jsonCodec, type Context, type Flow, type PersistenceSchema, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetCurrentWeatherStep, type Connection, type GetCurrentWeatherInput, type GetCurrentWeatherOutput } from '../../src/index.js';

/** Static Dex Web connection for the QWeather example. */
export const ConnectionName = 'qweather';
const weatherResult = new Attribute<GetCurrentWeatherOutput>('qweather-current-weather-result', jsonCodec<GetCurrentWeatherOutput>());

/** Read the current weather through QWeather. */
export class WeatherLookupFlow implements Flow<GetCurrentWeatherInput> {
  private readonly start = new StartWeatherLookup();
  private readonly complete = new CompleteWeatherLookup();
  /** Bind the static project connection. */
  constructor(private readonly connection: Connection) { if (connection.reference.name !== ConnectionName) throw new TypeError('QWeather connection name differs'); }
  /** Preserve the Flow identity. */ getFlowType(): string { return 'QWeatherWeatherLookup'; }
  /** Register the provider and terminal Steps. */
  getSteps(): StepList<GetCurrentWeatherInput> {
    return StepList.startStep(this.start).otherSteps(newGetCurrentWeatherStep({
      stepType: 'CallQWeather',
      annotations: { groupId: 'qweather', groupLabel: 'QWeather', explanation: 'Read the weather lookup with an API Key.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: new StepRef('CompleteQWeatherWeather'),
    }), this.complete);
  }
  /** Register the durable current weather. */ getPersistenceSchema(): PersistenceSchema { return { attributes: [weatherResult] }; }
}

// dex:group group-id:qweather group-label:"QWeather"
// dex:explanation text:"Start the current-weather query."
class StartWeatherLookup implements Step<GetCurrentWeatherInput> {
  getStepType(): string { return 'StartQWeatherWeather'; }
  execute(_context: Context, input: GetCurrentWeatherInput): StepDecision { return goTo(new StepRef('CallQWeather'), input); }
}

// dex:group group-id:qweather group-label:"QWeather"
// dex:explanation text:"Persist the current weather and complete the Flow."
class CompleteWeatherLookup implements Step<QueryResult<GetCurrentWeatherOutput>> {
  getStepType(): string { return 'CompleteQWeatherWeather'; }
  execute(context: Context, result: QueryResult<GetCurrentWeatherOutput>): StepDecision { weatherResult.set(context, result.value); return gracefulComplete(result.value); }
}
