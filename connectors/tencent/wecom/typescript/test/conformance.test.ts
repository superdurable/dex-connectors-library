// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { it } from 'vitest';
import { RetryError, runMutation, SecretString, StaticCredentialProvider, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type HeartbeatContext, type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
import { WeComClient } from '../src/client.js';
import { SendApplicationMessageInputSchema, SendGroupRobotMessageInputSchema } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../../conformance/connectors/tencent/wecom/scenarios.json', import.meta.url));
const robotConnection = { provider: 'wecom', name: 'wecom-robot' };
const applicationConnection = { provider: 'wecom', name: 'wecom-app' };
const clients = new WeakMap<ExchangeRecorder, WeComClient>();
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeWeCom));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeWeCom(scenario: Scenario, recorder: ExchangeRecorder, context: HeartbeatContext) {
  const credentials = new StaticCredentialProvider([
    [robotConnection, { authMethodId: 'group-robot', webhookKey: new SecretString('REDACTED-WEBHOOK-KEY') }],
    [applicationConnection, { authMethodId: 'application', corpId: 'ww1a2b3c4d5e6f7a8b', agentId: '1000002', corpSecret: new SecretString('REDACTED_CORP_SECRET') }],
  ]);
  const sender = (request: ProviderRequest) => answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  let client = clients.get(recorder);
  if (!client) {
    client = new WeComClient({ baseUrl: 'http://127.0.0.1:12345' }, credentials, { send: sender, now: () => new Date(context.fixedClock.getTime() + 1_000) });
    clients.set(recorder, client);
  }
  const connection = scenario.operation === 'sendGroupRobotMessage' ? robotConnection : applicationConnection;
  const call = scenarioCall(scenario, connection, 'wecom', context,
    { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'sendGroupRobotMessage') return productionMutation(client.sendGroupRobotMessage(), call, SendGroupRobotMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  if (scenario.operation === 'sendApplicationMessage') return productionMutation(client.sendApplicationMessage(), call, SendApplicationMessageInputSchema.parse(scenario.input), scenario.idempotencyKey!);
  throw new Error(`unknown WeCom operation ${scenario.operation}`);
}
