// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import type { Call } from '@superdurable/connectors-sdk';
import type { LoadedProject } from '@superdurable/connectors-sdk/projectconfig';
import { newProjectConnection } from '../src/project.js';

function project(configuration: unknown): LoadedProject {
  return {
    configuration: { connections: [{ connectorId: 'openweathermap', connectionName: 'openweathermap', provider: 'openweathermap', configuration }] },
    resolveCredential: async () => ({ api_key: 'test-key-123' }),
  } as unknown as LoadedProject;
}

const call = { context: { cancellationSignal: new AbortController().signal }, id: 'weather-project-test' } as Call;

describe('OpenWeatherMap project connection', () => {
  it.each([
    ['1m', 60_000],
    [60_000_000_000, 60_000],
  ])('uses manifest timeout %s for provider requests', async (timeout, expectedTimeoutMs) => {
    let requestTimeoutMs = 0;
    const connection = newProjectConnection(project({ timeout }), 'openweathermap', { send: async request => {
      requestTimeoutMs = request.timeoutMs ?? 0;
      return { status: 404, headers: {}, body: new Uint8Array() };
    } });
    await connection.operations.getCurrentWeather().invoke(call, { latitude: 1, longitude: 2 });
    expect(requestTimeoutMs).toBe(expectedTimeoutMs);
  });

  it.each([{ timeout: '1 minute' }, { timeout: 0 }, { timeout: true }, { unknownField: 'value' }])(
    'rejects invalid project configuration %#', configuration => {
      expect(() => newProjectConnection(project(configuration), 'openweathermap')).toThrow();
    },
  );
});
