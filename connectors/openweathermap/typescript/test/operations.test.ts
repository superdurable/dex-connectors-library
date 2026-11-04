// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { OpenWeatherMapClient, type OpenWeatherMapRequestSender } from '../src/index.js';

const call = { context: { cancellationSignal: new AbortController().signal }, id: 'weather-test' } as Call;
const credentials = { resolve: async () => ({ apiKey: new SecretString('test-key-123') }) };

describe('OpenWeatherMap operations', () => {
  it('selects terminal authentication failure without sending a request', async () => {
    const send: OpenWeatherMapRequestSender = async () => { throw new Error('unexpected provider request'); };
    const client = new OpenWeatherMapClient({}, { resolve: async () => { throw new Error('credential unavailable'); } }, { send });
    const result = await client.getCurrentWeather().invoke(call, { latitude: 1, longitude: 2 });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.kind).toBe('AUTHENTICATION'); }
  });

  it('rejects invalid coordinates before sending a request', async () => {
    const send: OpenWeatherMapRequestSender = async () => { throw new Error('unexpected provider request'); };
    const client = new OpenWeatherMapClient({}, credentials, { send });
    const result = await client.getForecast().invoke(call, { latitude: 91, longitude: 2 });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.kind).toBe('VALIDATION'); }
  });

  it('retries local network refusal as an availability failure', async () => {
    const send: OpenWeatherMapRequestSender = async () => { throw new Error('connect ECONNREFUSED'); };
    const client = new OpenWeatherMapClient({}, credentials, { send });
    const result = await client.getCurrentWeather().invoke(call, { latitude: 1, longitude: 2 });
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') expect(result.failure.kind).toBe('AVAILABILITY');
  });
});
