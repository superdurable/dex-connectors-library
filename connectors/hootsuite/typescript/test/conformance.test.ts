// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError } from '@superdurable/connectors-sdk/providerhttp';
import { HootsuiteClient } from '../src/client.js';
import { ListSocialProfilesInputSchema, ListScheduledPostsInputSchema, SchedulePostInputSchema } from '../src/generated/models.js';
import { answerRecordedRequest, invokeProductionMutation, loadScenarios, replay, scenarioCall, type ExchangeRecorder, type HeartbeatContext, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/hootsuite/scenarios.json', import.meta.url));
const connection = { provider: 'hootsuite', name: 'hootsuite' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeHootsuite));

async function invokeHootsuite(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const token = scenario.name === 'listSocialProfiles-defect' ? '' : 'test-hootsuite-token';
  const credentials: Array<[typeof connection, { oauthClientId: string; oauthClientSecret: SecretString; accessToken: SecretString; refreshToken: SecretString }]> = [[connection, {
    oauthClientId: 'test-client', oauthClientSecret: new SecretString('test-client-secret'),
    accessToken: new SecretString(token), refreshToken: new SecretString('test-refresh-token'),
  }]];
  const client = new HootsuiteClient({ baseUrl: 'http://127.0.0.1:12345' }, new StaticCredentialProvider(credentials), {
    send: request => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before)),
  });
  const call: Call = {
    context: { flowId: 'hootsuite-conformance', cancellationSignal: new AbortController().signal } as Call['context'],
    id: scenario.callId, connection, operation: { connectorId: 'hootsuite', operationId: scenario.operation },
    reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false,
  };
  if (scenario.operation === 'listSocialProfiles') return client.listSocialProfiles().invoke(call, ListSocialProfilesInputSchema.parse(scenario.input));
  if (scenario.operation === 'listScheduledPosts') return client.listScheduledPosts().invoke(call, ListScheduledPostsInputSchema.parse(scenario.input));
  if (scenario.operation === 'schedulePost') {
    const mutationCall = scenarioCall(scenario, connection, 'hootsuite', context) as Call;
    const input = SchedulePostInputSchema.parse(scenario.input);
    return invokeProductionMutation(client.schedulePost(), scenario.idempotencyKey!,
      recording => runMutation(recording as Mutation<typeof input, { id: string; state: string; socialProfileId: string }>, mutationCall, input), error => error instanceof RetryError);
  }
  throw new Error(`unknown Hootsuite operation ${scenario.operation}`);
}
