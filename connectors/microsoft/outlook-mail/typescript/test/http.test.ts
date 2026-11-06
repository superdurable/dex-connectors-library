// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe,expect,it } from 'vitest';
import { RetryError,SecretString,StaticCredentialProvider,type Call,type CredentialRefreshDriver } from '@superdurable/connectors-sdk';
import { BodyTooLargeError,ProviderTransportError,type ProviderRequest } from '@superdurable/connectors-sdk/providerhttp';
import { OutlookMailClient } from '../src/client.js';
import { escapePathSegment,escapeQueryComponent } from '../src/http.js';
import { graphTimestampUTC } from '../src/message.js';
import type { Credentials } from '../src/generated/models.js';
import { HeartbeatContext,scenarioCall,type Scenario } from '../../../../../conformance/runners/typescript/connector_scenarios.js';
const connection={provider:'microsoft',name:'outlook-mail-test'};
const credentials:Credentials={authMethodId:'microsoft-oauth',clientId:'client-id',clientSecret:new SecretString('client-secret'),accessToken:new SecretString('test-token'),refreshToken:new SecretString('refresh-token')};
const driver:CredentialRefreshDriver<Credentials>={refreshRequired:()=>false,refresh:async()=>{throw new Error('unexpected refresh');}};
function call():Call{const scenario={callId:'61a2326b-dea4-5c92-987e-58e7d98bc451',operation:'getMessage'} as Scenario;return scenarioCall(scenario,connection,'outlook-mail',new HeartbeatContext(undefined)) as Call;}
describe('Go-compatible Graph HTTP behavior',()=>{
  it('escapes OData query and path segments as Go does',()=>{expect(escapeQueryComponent('~* a:b')).toBe('~%2A%20a%3Ab');expect(escapePathSegment('a:b@c+d,e=f$g&h')).toBe('a:b@c+d%2Ce=f$g&h');});
  it('preserves nanosecond timestamps and rejects invalid calendar dates',()=>{expect(graphTimestampUTC('2026-01-01T12:00:00.123456789+01:30')).toBe('2026-01-01T10:30:00.123456789Z');expect(()=>graphTimestampUTC('2026-02-30T12:00:00Z')).toThrow();});
  it('classifies a read interrupted after a successful status as Retry',async()=>{const transport=async(request:ProviderRequest):Promise<never>=>{request.onResponseChunk?.(Buffer.from('{'),200,{});throw new ProviderTransportError(false);};const client=new OutlookMailClient({},new StaticCredentialProvider([[connection,credentials]]),{refreshDriver:driver,transport});const outcome=await client.getMessage().invoke(call(),{messageId:'message1'});expect(outcome).toMatchObject({kind:'retry',failure:{kind:'TRANSPORT'}});});
  it('classifies an oversized successful response as invalidResponse',async()=>{const transport=async():Promise<never>=>{throw new BodyTooLargeError(200,{});};const client=new OutlookMailClient({},new StaticCredentialProvider([[connection,credentials]]),{refreshDriver:driver,transport});const outcome=await client.getMessage().invoke(call(),{messageId:'message1'});expect(outcome).toMatchObject({kind:'branch',branch:'invalidResponse',failure:{kind:'RESPONSE_TOO_LARGE'}});});
  it('retries transient OAuth token exchange failures',async()=>{const source={resolve:async()=>credentials,resolveWithRefresh:async()=>{throw new RetryError({provider:'microsoft-graph',operation:'refreshCredentials',kind:'PROTOCOL',message:'token endpoint failed'});}};const client=new OutlookMailClient({},source,{refreshDriver:driver});const outcome=await client.getMessage().invoke(call(),{messageId:'message1'});expect(outcome).toMatchObject({kind:'retry',failure:{kind:'AVAILABILITY',message:'the Microsoft token endpoint is temporarily unavailable'}});});
  it('rejects unsafe response limits before any provider call',()=>{for(const limit of [NaN,Infinity,Number.MAX_SAFE_INTEGER+1])expect(()=>new OutlookMailClient({maxResponseBytes:limit},new StaticCredentialProvider([[connection,credentials]]))).toThrow();});
});
