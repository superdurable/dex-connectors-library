// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Call, type Failure, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import { GetCurrentWeatherDefinition, GetForecastDefinition } from './generated/definitions.js';
import type { CurrentWeatherOutput, ForecastOutput, WeatherInput } from './generated/models.js';
import type { OpenWeatherMapClient } from './client.js';

const emptyCurrent: CurrentWeatherOutput = { city: '', temperature: 0, description: '' };
const emptyForecast: ForecastOutput = { city: '', entries: [] };

/** Read current temperature and condition by coordinates. */
export class GetCurrentWeatherOperation implements Query<WeatherInput, CurrentWeatherOutput> {
  /** Bind an authenticated provider client. */ constructor(private readonly client: OpenWeatherMapClient) {}
  /** Return the generated operation contract. */ definition() { return GetCurrentWeatherDefinition; }
  /** Validate and invoke one provider request. */
  invoke(call: Call, input: WeatherInput): Promise<QueryAttempt<CurrentWeatherOutput>> {
    const query = weatherQuery(input);
    if (typeof query === 'string') return Promise.resolve(defect('getCurrentWeather', emptyCurrent, query));
    return this.client.query(call, 'getCurrentWeather', '/data/2.5/weather', query, emptyCurrent, decodeCurrent);
  }
}

/** Read five days of three-hour forecast entries. */
export class GetForecastOperation implements Query<WeatherInput, ForecastOutput> {
  /** Bind an authenticated provider client. */ constructor(private readonly client: OpenWeatherMapClient) {}
  /** Return the generated operation contract. */ definition() { return GetForecastDefinition; }
  /** Validate and invoke one provider request. */
  invoke(call: Call, input: WeatherInput): Promise<QueryAttempt<ForecastOutput>> {
    const query = weatherQuery(input);
    if (typeof query === 'string') return Promise.resolve(defect('getForecast', emptyForecast, query));
    return this.client.query(call, 'getForecast', '/data/2.5/forecast', query, emptyForecast, decodeForecast);
  }
}

function weatherQuery(input: WeatherInput): URLSearchParams | string {
  if (!Number.isFinite(input.latitude) || input.latitude < -90 || input.latitude > 90 || !Number.isFinite(input.longitude) || input.longitude < -180 || input.longitude > 180) return 'latitude or longitude is outside its degree range';
  const units = input.units || 'standard';
  if (!['standard', 'metric', 'imperial'].includes(units)) return 'units must be standard, metric, or imperial';
  return new URLSearchParams({ lat: goDecimal(input.latitude), lon: goDecimal(input.longitude), units });
}
function goDecimal(value: number): string {
  if (Object.is(value, -0)) return '-0';
  const shortest = String(value);
  if (!shortest.includes('e')) return shortest;
  const [mantissa = '', exponentText = '0'] = shortest.split('e');
  const exponent = Number(exponentText);
  const isNegative = mantissa.startsWith('-');
  const digits = mantissa.replace('-', '').replace('.', '');
  const decimalPosition = mantissa.replace('-', '').split('.')[0]!.length + exponent;
  const expanded = decimalPosition <= 0 ? `0.${'0'.repeat(-decimalPosition)}${digits}`
    : decimalPosition >= digits.length ? `${digits}${'0'.repeat(decimalPosition - digits.length)}`
      : `${digits.slice(0, decimalPosition)}.${digits.slice(decimalPosition)}`;
  return isNegative ? `-${expanded}` : expanded;
}
function defect<Output>(operation: string, empty: Output, message: string): QueryAttempt<Output> {
  const failure: Failure = { kind: 'VALIDATION', provider: 'openweathermap', operation, message };
  return newQueryBranch('defect', empty, {}, failure);
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError('invalid provider object');
  return value as Record<string, unknown>;
}
function number(value: unknown): number {
  if (typeof value !== 'number') throw new TypeError('invalid provider number');
  return value;
}
function stringOrZero(value: unknown): string {
  if (value === undefined || value === null) return '';
  if (typeof value !== 'string') throw new TypeError('invalid provider string');
  return value;
}
function decodeCurrent(body: Uint8Array): CurrentWeatherOutput {
  const wire = object(JSON.parse(Buffer.from(body).toString('utf8')));
  const main = object(wire.main ?? {});
  if (!Array.isArray(wire.weather) || wire.weather.length === 0) throw new TypeError('weather fields missing');
  const firstWeather = wire.weather[0] === null ? {} : object(wire.weather[0]);
  return { city: stringOrZero(wire.name), temperature: number(main.temp), description: stringOrZero(firstWeather.description) };
}
function decodeForecast(body: Uint8Array): ForecastOutput {
  const wire = object(JSON.parse(Buffer.from(body).toString('utf8')));
  if (!Array.isArray(wire.list)) throw new TypeError('forecast list missing');
  const entries = wire.list.map(item => {
    const point = object(item);
    const timestamp = typeof point.dt === 'number' ? point.dt : 0;
    if (!Number.isSafeInteger(timestamp) || timestamp <= 0) throw new TypeError('forecast point incomplete');
    return { timestamp, temperature: number(object(point.main ?? {}).temp) };
  });
  return { city: stringOrZero(object(wire.city ?? {}).name), entries };
}
