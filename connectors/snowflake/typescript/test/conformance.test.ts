// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createPublicKey, generateKeyPairSync, verify } from 'node:crypto';
import { expect, it } from 'vitest';
import { RetryError, runMutation, SecretString, type Call, type Mutation } from '@superdurable/connectors-sdk';
import { ProviderTransportError, type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { answerRecordedRequest, ExchangeRecorder, invokeProductionMutation, loadScenarios, replay, scenarioCall, type Scenario } from '../../../../conformance/runners/typescript/connector_scenarios.js';
import { SnowflakeClient } from '../src/client.js';
import { SubmitStatementInputSchema, GetStatementResultInputSchema, CancelStatementInputSchema, type SubmitStatementInput, type CancelStatementInput } from '../src/generated/models.js';

const scenarios = loadScenarios(new URL('../../../../conformance/connectors/snowflake/scenarios.json', import.meta.url));
const connection = { provider: 'snowflake', name: 'warehouse' };
it.each(scenarios)('matches Go conformance: $name', scenario => replay(scenario, invokeSnowflake));

function productionMutation<Input, Output>(operation: Mutation<Input, Output>, call: Call, input: Input, key: string) {
  return invokeProductionMutation(operation, key,
    recording => runMutation(recording as Mutation<Input, Output>, call, input), error => error instanceof RetryError);
}

async function invokeSnowflake(scenario: Scenario, recorder: ExchangeRecorder) {
  const isKeyPairScenario = scenario.name === 'submit-key-pair-jwt';
  const privateKey = isKeyPairScenario ? generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey : undefined;
  const sender = (request: ProviderRequest) => {
    if (privateKey) {
      const token = request.headers?.Authorization?.replace(/^Bearer /, '') ?? '';
      const [header, payload, signature] = token.split('.');
      expect(JSON.parse(Buffer.from(header ?? '', 'base64url').toString())).toEqual({ alg: 'RS256', typ: 'JWT' });
      const claims = JSON.parse(Buffer.from(payload ?? '', 'base64url').toString()) as Record<string, unknown>;
      expect(claims.sub).toBe('MYORG-ANALYTICS.DEX_SERVICE');
      expect(claims.iat).toBe(1790769600);
      expect(claims.exp).toBe(1790773140);
      const publicDER = createPublicKey(privateKey).export({ type: 'spki', format: 'der' });
      expect(claims.iss).toBe(`MYORG-ANALYTICS.DEX_SERVICE.SHA256:${createHash('sha256').update(publicDER).digest('base64')}`);
      expect(verify('RSA-SHA256', Buffer.from(`${header}.${payload}`), privateKey, Buffer.from(signature ?? '', 'base64url'))).toBe(true);
      request = { ...request, headers: { ...request.headers, Authorization: 'Bearer conformance-key-pair-jwt' } };
    }
    return answerRecordedRequest(recorder, request, before => new ProviderTransportError(before));
  };
  const client = new SnowflakeClient({ accountIdentifier: 'myorg-analytics', warehouse: 'REPORTING_WH', role: 'DEX_REPORTING', database: 'ANALYTICS', schema: 'BILLING' }, {
    resolve: async () => privateKey
      ? { authMethodId: 'key-pair', user: 'dex_service', privateKey: new SecretString(privateKey.export({ type: 'pkcs8', format: 'pem' }).toString()) }
      : { authMethodId: 'programmatic-access-token', programmaticAccessToken: new SecretString('pat-secret-value-0123456789') },
  }, { localProviderUrl: 'http://127.0.0.1', send: sender, now: () => new Date('2026-09-30T12:00:00Z') });
  const call = scenarioCall(scenario, connection, 'snowflake', {
    flowId: 'snowflake-conformance', cancellationSignal: new AbortController().signal,
  }, { reportProgress() {}, writeText() {}, hasProgressStream: () => false, hasTextStream: () => false }) as Call;
  if (scenario.operation === 'submitStatement') return productionMutation(client.submitStatement(), call, SubmitStatementInputSchema.parse(scenario.input) as SubmitStatementInput, scenario.idempotencyKey!);
  if (scenario.operation === 'getStatementResult') return client.getStatementResult().invoke(call, GetStatementResultInputSchema.parse(scenario.input));
  if (scenario.operation === 'cancelStatement') return productionMutation(client.cancelStatement(), call, CancelStatementInputSchema.parse(scenario.input) as CancelStatementInput, scenario.idempotencyKey!);
  throw new Error(`unknown Snowflake operation ${scenario.operation}`);
}
