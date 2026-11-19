// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StepList, gracefulComplete, type Context, type Flow, type Step, type StepDecision } from '@superdurable/dex';
import type { QueryResult } from '@superdurable/connectors-sdk';
import { newGetCurrentWeatherStep, type Connection, type WeatherInput, type CurrentWeatherOutput } from '@superdurable/connector-openweathermap';

/** Static Dex Web connection name. */
export const ConnectionName = 'openweathermap';

/** One current-weather Flow backed by the provider Step. */
export class WeatherFlow implements Flow<WeatherInput> {
  private readonly complete = new CompleteWeather();
  /** Bind the provider connection. */
  constructor(private readonly connection: Connection) {
    if (!connection) throw new TypeError('OpenWeatherMap connection is required');
  }
  /** Stable Flow type. */ getFlowType(): string { return 'OpenWeatherMapTypeScriptWeather'; }
  /** Register the provider and completion Steps. */
  getSteps(): StepList<WeatherInput> {
    return StepList.startStep(newGetCurrentWeatherStep<WeatherInput>({
      stepType: 'ReadOpenWeatherMapCurrentWeather',
      annotations: { groupId: 'weather', groupLabel: 'Weather', explanation: 'Read the current weather for the requested coordinates.' },
      connection: this.connection, connectionName: ConnectionName,
      mapToOperationInput: input => input, found: this.complete,
    })).otherSteps(this.complete);
  }
}

/** Complete the Flow with safe weather data. */
// dex:group group-id:weather group-label:"Weather"
// dex:explanation text:"Return the current condition after the provider read completes."
export class CompleteWeather implements Step<QueryResult<CurrentWeatherOutput>> {
  /** Stable completion Step type. */ getStepType(): string { return 'CompleteOpenWeatherMapWeather'; }
  /** Return the weather result. */ execute(_context: Context, result: QueryResult<CurrentWeatherOutput>): StepDecision { return gracefulComplete(result.value); }
}
