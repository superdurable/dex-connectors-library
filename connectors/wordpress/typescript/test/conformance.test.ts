// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import {
  WordPressClient, CreatePostInputSchema, GetPostInputSchema, ListPostsInputSchema,
  type CreatedPost,
} from '../src/index.js';
import {
  answerRecordedRequest, invokeProductionMutation, loadScenarios, replay, scenarioCall,
  type ExchangeRecorder, type HeartbeatContext, type Scenario,
} from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/wordpress/scenarios.json', import.meta.url));
const connection = { provider: 'wordpress', name: 'wordpress' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWordPress));

async function invokeWordPress(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  const client = new WordPressClient({ siteUrl: 'http://127.0.0.1:12345' },
    { resolve: async () => ({ username: 'editor', applicationPassword: new SecretString('synthetic test password') }) },
    { send: sender });
  const call = scenarioCall(scenario, connection, 'wordpress', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'createPost') {
    const input = CreatePostInputSchema.parse(scenario.input);
    return invokeProductionMutation(client.createPost(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, CreatedPost>, call, input),
      error => error instanceof RetryError);
  }
  if (scenario.operation === 'getPost') return client.getPost().invoke(call, GetPostInputSchema.parse(scenario.input));
  if (scenario.operation === 'listPosts') return client.listPosts().invoke(call, ListPostsInputSchema.parse(scenario.input));
  throw new Error(`unknown WordPress operation ${scenario.operation}`);
}
