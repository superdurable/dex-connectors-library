// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from 'vitest';
import { SecretString, type Call } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { ElevenLabsClient, type ElevenLabsRequestSender } from '../src/client.js';

const call = {
  id: 'test-call', connection: { provider: 'elevenlabs', name: 'elevenlabs' },
  operation: { connectorId: 'elevenlabs', operationId: 'listVoices' },
  context: { cancellationSignal: new AbortController().signal },
} as Call;

function client(send: ElevenLabsRequestSender) {
  return new ElevenLabsClient({ baseUrl: 'http://127.0.0.1' }, { resolve: async () => ({ apiKey: new SecretString('test-key-123') }) }, { send });
}

describe('ElevenLabs provider classification', () => {
  it('selects an authentication defect before any request when credentials are unavailable', async () => {
    let sent = false;
    const provider = new ElevenLabsClient({}, { resolve: async () => { throw new Error('private credential error'); } }, {
      send: async () => { sent = true; throw new Error('unexpected'); },
    });
    const result = await provider.listVoices().invoke(call, {});
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.branch).toBe('defect');
      expect(result.failure?.kind).toBe('AUTHENTICATION');
      expect(result.failure?.message).not.toContain('private');
    }
    expect(sent).toBe(false);
  });

  it('retries a local connection refusal just like the Go client', async () => {
    const provider = client(async () => { throw new ProviderTransportError(false); });
    const result = await provider.listVoices().invoke(call, {});
    expect(result.kind).toBe('retry');
    if (result.kind === 'retry') expect(result.failure.kind).toBe('AVAILABILITY');
  });

  it('classifies an oversized success as invalidResponse', async () => {
    const provider = client(async () => { throw new BodyTooLargeError(200, {}); });
    const result = await provider.createSpeech().invoke(call, { voiceId: 'voice_meridian', text: 'Hello' });
    expect(result.kind).toBe('branch');
    if (result.kind === 'branch') {
      expect(result.branch).toBe('invalidResponse');
      expect(result.failure?.kind).toBe('RESPONSE_TOO_LARGE');
    }
  });
});
