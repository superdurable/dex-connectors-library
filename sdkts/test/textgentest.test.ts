// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { textGenerationBranchDefinitions } from '../src/textgen/contract.js';
import { newWireFormat } from '../src/textgen/openaichat/index.js';
import { newTextGenerationQuery } from '../src/textgen/query.js';
import { runTextGenerationExchangeSuite, type GeneratedReply, type ProviderDialect } from '../src/textgen/textgentest/index.js';

function streamReply(reply: GeneratedReply, reason: string) {
  return {
    headers: { 'Content-Type': 'text/event-stream', 'x-request-id': 'req-textgentest-1' },
    body: `data: ${JSON.stringify({
      id: reply.responseId, model: reply.servedModel,
      choices: [{ index: 0, delta: { content: reply.text }, finish_reason: reason }],
      usage: {
        prompt_tokens: reply.usage.inputTokens, completion_tokens: reply.usage.outputTokens,
        total_tokens: reply.usage.totalTokens,
        prompt_tokens_details: { cached_tokens: reply.usage.cachedInputTokens },
        completion_tokens_details: { reasoning_tokens: reply.usage.reasoningTokens },
      },
    })}\n\ndata: [DONE]\n\n`,
  };
}

const dialect: ProviderDialect = {
  credentialHeader: { name: 'Authorization', prefix: 'Bearer ' },
  connectionModel: 'chat-model', alternateModel: 'alternate-chat-model', requestIDHeader: 'x-request-id',
  readRequestModel: request => (JSON.parse(request.body.toString()) as { model: string }).model,
  generatedReply: reply => streamReply(reply, 'stop'),
  truncatedReply: reply => streamReply(reply, 'length'),
  blockedReply: reply => streamReply(reply, 'content_filter'),
  errorReply: (statusCode, message) => ({ statusCode, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ error: { code: 'provider_error', message } }) }),
  quotaExhaustedReply: message => ({ statusCode: 402, body: JSON.stringify({ error: { code: 'quota_exhausted', message } }) }),
  malformedReply: () => ({ headers: { 'Content-Type': 'text/event-stream' }, body: 'data: malformed\n\n' }),
  reportedErrorReply: (token, message) => ({ headers: { 'Content-Type': 'text/event-stream' }, body: `data: ${JSON.stringify({ error: { code: token, message } })}\n\n` }),
  interruptedStreamReply: reply => ({ headers: { 'Content-Type': 'text/event-stream' }, body: `data: ${JSON.stringify({ choices: [{ index: 0, delta: { content: reply.text } }] })}\n\n` }),
  unstreamedReply: reply => ({ headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({
    id: reply.responseId, model: reply.servedModel,
    choices: [{ index: 0, message: { content: reply.text }, finish_reason: 'stop' }],
    usage: { prompt_tokens: reply.usage.inputTokens, completion_tokens: reply.usage.outputTokens, total_tokens: reply.usage.totalTokens,
      prompt_tokens_details: { cached_tokens: reply.usage.cachedInputTokens }, completion_tokens_details: { reasoning_tokens: reply.usage.reasoningTokens } },
  }) }),
  optionalRequestFieldPointers: ['/temperature', '/max_tokens', '/reasoning_effort', '/response_format'],
};

it('replays the shared Go textgentest exchange contract for OpenAI Chat', async () => {
  await runTextGenerationExchangeSuite({
    dialect,
    newQuery: connection => newTextGenerationQuery({
      definition: { operation: { connectorId: 'textgentest-chat', operationId: 'generateText' },
        branches: textGenerationBranchDefinitions(), stepDefaults: { executeDurability: 'sync', executeMethodTimeoutMs: 30_000 } },
      wireFormat: newWireFormat({ providerName: 'textgentest-chat', streaming: 'always', structuredOutput: { mode: 'jsonSchema' } }),
      baseURL: connection.baseURL, connectionModel: connection.model, requestTimeoutMs: 10_000,
      resolveCredential: () => connection.apiKey, maxResponseBytes: connection.maxResponseBytes, maxStreamEventBytes: 4096,
    }),
    locallyRejectedRequests: [{ messages: [] }],
  });
});
