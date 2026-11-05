// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { newQueryBranch, type Call, type Query, type QueryAttempt } from '@superdurable/connectors-sdk';
import type { ElevenLabsClient } from './client.js';
import { CreateSpeechDefinition, ListVoicesDefinition } from './generated/definitions.js';
import type { CreateSpeechInput, CreateSpeechOutput, ListVoicesInput, ListVoicesOutput, Voice } from './generated/models.js';

const emptyVoices: ListVoicesOutput = { voices: [], hasMore: false };
const emptySpeech: CreateSpeechOutput = { audioBase64: '', contentType: '' };
const voiceIDPattern = /^[A-Za-z0-9_-]{1,128}$/;

/** List one page of voices using the Go connector's validation and response rules. */
export class ListVoicesOperation implements Query<ListVoicesInput, ListVoicesOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: ElevenLabsClient) {}
  /** Return the generated manifest definition. */ definition() { return ListVoicesDefinition; }
  /** Validate and fetch one voice page. */
  invoke(call: Call, input: ListVoicesInput): Promise<QueryAttempt<ListVoicesOutput>> {
    const pageSize = input.pageSize ?? 0;
    const token = input.nextPageToken ?? '';
    if (!Number.isSafeInteger(pageSize) || pageSize < 0 || pageSize > 100 || typeof token !== 'string' || /[\r\n]/.test(token)) {
      return Promise.resolve(newQueryBranch('defect', emptyVoices, {}, { kind: 'VALIDATION', provider: 'elevenlabs', operation: 'listVoices', message: 'page size or token is invalid' }));
    }
    const query = new URLSearchParams();
    if (token) query.set('next_page_token', token);
    if (pageSize > 0) query.set('page_size', String(pageSize));
    return this.client.invokeQuery(call, 'listVoices', 'listed', 'GET', '/v2/voices', query, undefined, emptyVoices, decodeVoices);
  }
}

/** Generate bounded MP3 audio from one voice and text. */
export class CreateSpeechOperation implements Query<CreateSpeechInput, CreateSpeechOutput> {
  /** Bind an authenticated client. */ constructor(private readonly client: ElevenLabsClient) {}
  /** Return the generated manifest definition. */ definition() { return CreateSpeechDefinition; }
  /** Validate and synthesize one response. */
  invoke(call: Call, input: CreateSpeechInput): Promise<QueryAttempt<CreateSpeechOutput>> {
    if (typeof input.voiceId !== 'string' || !voiceIDPattern.test(input.voiceId) || typeof input.text !== 'string' || !input.text.trim() || (input.modelId !== undefined && typeof input.modelId !== 'string')) {
      return Promise.resolve(newQueryBranch('defect', emptySpeech, {}, { kind: 'VALIDATION', provider: 'elevenlabs', operation: 'createSpeech', message: 'voiceId and text are required' }));
    }
    const body = Buffer.from(JSON.stringify({ text: input.text, model_id: input.modelId || 'eleven_multilingual_v2' }), 'utf8');
    return this.client.invokeQuery(call, 'createSpeech', 'created', 'POST', `/v1/text-to-speech/${input.voiceId}`, new URLSearchParams({ output_format: 'mp3_44100_128' }), body, emptySpeech, decodeSpeech);
  }
}

function decodeVoices(body: Uint8Array): ListVoicesOutput {
  const document: unknown = JSON.parse(Buffer.from(body).toString('utf8'));
  if (!isObject(document) || !Array.isArray(document.voices)) throw new TypeError('voices missing');
  const voices: Voice[] = document.voices.map(voice => {
    if (!isObject(voice) || typeof voice.voice_id !== 'string' || !voice.voice_id || (voice.name != null && typeof voice.name !== 'string')) throw new TypeError('voice invalid');
    return { voiceId: voice.voice_id, name: voice.name ?? '' };
  });
  if (document.has_more != null && typeof document.has_more !== 'boolean') throw new TypeError('has_more invalid');
  if (document.next_page_token != null && typeof document.next_page_token !== 'string') throw new TypeError('next_page_token invalid');
  return { voices, hasMore: document.has_more ?? false, ...(document.next_page_token ? { nextPageToken: document.next_page_token } : {}) };
}

function decodeSpeech(body: Uint8Array): CreateSpeechOutput {
  if (!body.length) throw new TypeError('audio missing');
  return { audioBase64: Buffer.from(body).toString('base64'), contentType: 'audio/mpeg' };
}

function isObject(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value); }
