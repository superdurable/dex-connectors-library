// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { afterEach, describe, expect, it } from 'vitest';
import { createServer, type Server } from 'node:http';
import { readFileSync } from 'node:fs';
import { createCall } from '../src/credentials.js';
import { newTextGenerationQuery } from '../src/textgen/query.js';
import { canonicalizePortableSchema, extractStructuredAnswer, transformSchemaForProvider } from '../src/textgen/structured_output.js';
import { classifyError, blockedOutcome, quotaExhaustedOutcome, temperatureRange, ProviderReportedError, type WireFormat } from '../src/textgen/wire_format.js';
import { textGenerationBranchDefinitions, validateModelID, type TextGenerationRequest } from '../src/textgen/contract.js';
import type { TextGenerationQueryConfig } from '../src/textgen/query.js';
import type { AsyncContext } from '@superdurable/dex';

const cases = JSON.parse(readFileSync(new URL('../../sdkgo/textgen/textgentest/testdata/model_id_cases.json', import.meta.url),'utf8')) as {cases:{name:string;rule:'body'|'pathSegment';input:string;valid:boolean;modelId?:string}[]};
for (const testCase of cases.cases) it(`Go model ID fixture: ${testCase.rule}: ${testCase.name}`, () => {
  if (testCase.valid) expect(validateModelID(testCase.rule,testCase.input)).toBe(testCase.modelId);
  else expect(() => validateModelID(testCase.rule,testCase.input)).toThrow();
});
const schema = { type:'object',properties:{ n:{type:'integer',minimum:1}, tags:{type:'array',items:{type:'string'},minItems:1} },required:['n','tags'],additionalProperties:false };
const request = (): TextGenerationRequest => ({ messages:[{role:'user',text:'hello'}] });
const call = () => createCall({ flowId:'textgen-flow',stepExecutionId:'textgen-step',attempt:1,recordHeartbeat:async () => {} } as unknown as AsyncContext,{provider:'test',name:'pipeline'},{connectorId:'test-lab',operationId:'generateText'});
const wireFormat = (): WireFormat => ({
  providerName:'test-lab',modelIDRule:'body',features:{supportsInstructions:true,supportsStructuredOutput:true,supportsMaxOutputTokens:true,supportsTemperature:true,supportsReasoningEffort:true},credentialHeader:{name:'Authorization',prefix:'Bearer '},
  rulesForModel:model => ({temperature:model.startsWith('fixed-') ? {kind:'notAccepted'} : temperatureRange(0,1),reasoningEfforts:{low:'wire-low'},structuredOutput:{mode:'jsonSchema'}}),
  encodeRequest:({request:input}) => ({path:'/generate',body:Buffer.from(JSON.stringify(input)),isStreaming:false}),
  decodeResponse:body => { const decoded = JSON.parse(Buffer.from(body).toString()) as {text?:string;reasoning?:string;finish?:string;model?:string;id?:string;refusal?:boolean;usage?:Record<string,number>;error?:{code?:string}}; if (decoded.error) throw new ProviderReportedError([decoded.error.code ?? '']); return {parts:[{text:decoded.reasoning ?? '',isReasoning:true},{text:decoded.text ?? ''}],servedModel:decoded.model ?? '',responseId:decoded.id ?? '',providerFinishReason:decoded.finish ?? '',isRefusal:decoded.refusal ?? false,usage:decoded.usage ?? {}}; },
  decodeStreamEvent:() => {},
  finishReasons:{done:'stop',cut:'length',filtered:'contentPolicy'},errorTokenPointers:['/error/code'],requestIDHeaders:['x-first-request-id','x-request-id'],rateLimitHeaders:['x-ratelimit-remaining','x-ratelimit-echo'],
});
let server: Server | undefined;
afterEach(async () => { if (server) { await new Promise<void>(resolve => server?.close(() => resolve())); server = undefined; } });
async function fixture(status = 200, body: unknown = {text:'hello',reasoning:'hidden',finish:'done',model:'served',id:'resp-1'}, extra?: (config: TextGenerationQueryConfig) => void) {
  const received: {path:string;body:string;headers:Record<string,unknown>}[] = [];
  server = createServer(async (incoming,response) => { const chunks: Buffer[] = []; for await (const chunk of incoming) chunks.push(chunk as Buffer); received.push({path:incoming.url ?? '',body:Buffer.concat(chunks).toString(),headers:incoming.headers}); response.setHeader('x-request-id','req-1'); response.setHeader('x-ratelimit-remaining','41'); response.statusCode = status; response.setHeader('content-type','application/json'); response.end(JSON.stringify(body)); });
  await new Promise<void>(resolve => server?.listen(0,'127.0.0.1',resolve));
  const address = server.address(); if (!address || typeof address === 'string') throw new Error('missing address');
  const config: TextGenerationQueryConfig = { definition:{operation:{connectorId:'test-lab',operationId:'generateText'},branches:textGenerationBranchDefinitions(),stepDefaults:{executeDurability:'sync',executeMethodTimeoutMs:60000}},wireFormat:wireFormat(),baseURL:`http://127.0.0.1:${address.port}/v1`,connectionModel:' test-model ',requestTimeoutMs:10000,resolveCredential:()=>'sk-pipeline-test-key',maxResponseBytes:65536,maxStreamEventBytes:4096 };
  extra?.(config);
  return {query:newTextGenerationQuery(config),received};
}
describe('Go pipeline scenarios', () => {
  it('validates, resolves the model, decodes, and records safe receipt fields', async () => {
    const {query,received} = await fixture();
    const result = await query.invoke(call(),request());
    expect(result.kind).toBe('branch');
    if (result.kind !== 'branch') return;
    expect(result.branch).toBe('generated'); expect(result.value.text).toBe('hello'); expect(result.value.requestedModel).toBe('test-model');
    expect(result.value.servedModel).toBe('served'); expect(result.receipt.providerObjectId).toBe('resp-1'); expect(result.receipt.providerRequestId).toBe('req-1');
    expect(result.receipt.metadata).toEqual({'x-ratelimit-remaining':'41'}); expect(received[0]?.path).toBe('/v1/generate');
    expect(received[0]?.headers.authorization).toBe('Bearer sk-pipeline-test-key');
  });
  it.each([
    ['missing messages',{messages:[]}],['bad role',{messages:[{role:'system',text:'hi'}]}],['empty text',{messages:[{role:'user',text:''}]}],
    ['negative token cap',{...request(),maxOutputTokens:-1}],['unsupported effort',{...request(),reasoningEffort:'high'}],['temperature beyond model range',{...request(),temperature:1.5}],
    ['fixed temperature',{...request(),model:'fixed-model',temperature:0.5}],['unsupported schema keyword',{...request(),structuredOutput:{name:'count',schema:{...schema,$ref:'#/defs/x'}}}],
  ])('rejects %s before any provider request', async (_name,input) => {
    const {query,received} = await fixture();
    const result = await query.invoke(call(),input as TextGenerationRequest);
    expect(result.kind).toBe('branch'); if (result.kind === 'branch') { expect(result.branch).toBe('defect'); expect(result.failure?.kind).toBe('VALIDATION'); }
    expect(received).toHaveLength(0);
  });
  it.each([
    [408,'retry','AVAILABILITY'],[429,'retry','RATE_LIMIT'],[500,'retry','AVAILABILITY'],[503,'retry','AVAILABILITY'],
    [401,'providerRejected','AUTHENTICATION'],[402,'providerRejected','QUOTA_EXHAUSTED'],[403,'providerRejected','AUTHORIZATION'],[404,'providerRejected','NOT_FOUND'],[409,'providerRejected','CONFLICT'],[501,'providerRejected','PROVIDER_REJECTION'],[302,'invalidResponse','PROTOCOL'],
  ])('classifies HTTP %i', async (status, expected, kind) => {
    const {query} = await fixture(status,{error:{code:'lab_error',message:'sensitive text'}});
    const result = await query.invoke(call(),request());
    expect(result.kind === 'retry' ? 'retry' : result.branch).toBe(expected);
    expect(result.failure?.kind).toBe(kind);
    expect(result.failure?.message).not.toContain('sensitive');
  });
  it('applies first matching error rule and status-independent reported errors', () => {
    const rules = [{statusCode:429,errorToken:'quota',outcome:quotaExhaustedOutcome()},{statusCode:0,errorToken:'blocked',outcome:blockedOutcome()}];
    expect(classifyError(rules,429,['quota']).kind).toBe('QUOTA_EXHAUSTED');
    expect(classifyError(rules,0,['blocked'],true).disposition).toBe('blocked');
  });
  it('prefers a provider retry delay and caps it at one hour', async () => {
    const {query} = await fixture(429,{error:{code:'slow_down'}},config => { config.wireFormat.readErrorRetryDelay = () => 4_000_000; });
    const outcome = await query.invoke(call(),request());
    expect(outcome.kind).toBe('retry');
    if (outcome.kind === 'retry') { expect(outcome.failure.kind).toBe('RATE_LIMIT'); expect(outcome.retryAfterMs).toBe(3_600_000); }
  });
  it('classifies an error status independently of the successful-body size limit', async () => {
    const {query} = await fixture(429,{error:{code:'rate_limit',message:'x'.repeat(100)}},config => { config.maxResponseBytes = 1; });
    const outcome = await query.invoke(call(),request());
    expect(outcome.kind).toBe('retry');
    if (outcome.kind === 'retry') expect(outcome.failure.kind).toBe('RATE_LIMIT');
  });
  it('classifies an error envelope inside a 2xx response using status-independent rules', async () => {
    const {query} = await fixture(200,{error:{code:'blocked',message:'secret provider text'}},config => { config.wireFormat.errorRules = [{statusCode:0,errorToken:'blocked',outcome:blockedOutcome()}]; });
    const outcome = await query.invoke(call(),request());
    expect(outcome.kind).toBe('branch');
    if (outcome.kind === 'branch') { expect(outcome.branch).toBe('blocked'); expect(outcome.failure?.message).not.toContain('secret provider text'); }
  });
  it('canonicalizes a one-shot schema value once for request and response', async () => {
    let marshals = 0;
    const singleUse = { toJSON: () => { if (++marshals > 1) throw new Error('schema serialized more than once'); return 'shipped'; } };
    const structured = {type:'object',properties:{status:{type:'string',enum:[singleUse]}},required:['status'],additionalProperties:false};
    const {query} = await fixture(200,{text:'{"status":"shipped"}',finish:'done'});
    const result = await query.invoke(call(),{...request(),structuredOutput:{name:'status',schema:structured}});
    expect(marshals).toBe(1);
    expect(result.kind).toBe('branch'); if (result.kind === 'branch') expect(result.branch).toBe('generated');
  });
  it('records nil heartbeats only while a slow provider attempt is in flight', async () => {
    const received: unknown[] = [];
    server = createServer(async (incoming,response) => {
      for await (const chunk of incoming) { void chunk; }
      await new Promise<void>(resolve => setTimeout(resolve, 5200));
      response.setHeader('Content-Type','application/json');
      response.end(JSON.stringify({text:'ready',finish:'done'}));
    });
    await new Promise<void>(resolve => server?.listen(0,'127.0.0.1',resolve));
    const address = server.address(); if (!address || typeof address === 'string') throw new Error('missing address');
    const query = newTextGenerationQuery({definition:{operation:{connectorId:'test-lab',operationId:'generateText'},branches:textGenerationBranchDefinitions(),stepDefaults:{executeDurability:'sync',executeMethodTimeoutMs:20000,heartbeatTimeoutMs:10000}},wireFormat:wireFormat(),baseURL:`http://127.0.0.1:${address.port}`,connectionModel:'test-model',requestTimeoutMs:10000,resolveCredential:()=>'test-only',maxResponseBytes:65536,maxStreamEventBytes:4096});
    const providerCall = call();
    (providerCall.context as unknown as {recordHeartbeat:(value:unknown)=>Promise<void>}).recordHeartbeat = async value => { received.push(value); };
    const outcome = await query.invoke(providerCall,request());
    expect(outcome.kind).toBe('branch');
    expect(received).toEqual([undefined]);
  }, 8000);
  it.each([
    ['truncated',{text:'partial',finish:'cut'},'truncated'],['content policy',{text:'secret',finish:'filtered'},'blocked'],['refusal',{text:'secret',refusal:true},'blocked'],['missing text',{finish:'done'},'invalidResponse'],['unknown finish',{text:'answer',finish:'unknown'},'invalidResponse'],['negative usage',{text:'answer',finish:'done',usage:{inputTokens:-1}},'invalidResponse'],
  ])('classifies %s response', async (_name,body,expected) => {
    const {query} = await fixture(200,body); const result = await query.invoke(call(),request());
    expect(result.kind).toBe('branch'); if (result.kind === 'branch') { expect(result.branch).toBe(expected); if (expected === 'blocked') expect(result.value.text).toBe(''); }
  });
});
describe('Go structured output scenarios', () => {
  it('transforms a provider schema without mutating local constraints', () => {
    const canonical = canonicalizePortableSchema(schema);
    const transformed = transformSchemaForProvider(canonical,{mode:'jsonSchema',keywordsMovedToDescription:['minimum'],keywordsRemoved:['minItems']});
    expect((canonical.properties as Record<string,Record<string,unknown>>).n?.minimum).toBe(1);
    expect((transformed.properties as Record<string,Record<string,unknown>>).n?.description).toBe('(minimum: 1)');
  });
  it.each([
    ['fence','```json\n{"n":2,"tags":["x"]}\n```',true],['exact exponent','{"n":3e0,"tags":["x"]}',true],
    ['too small','{"n":0,"tags":["x"]}',false],['missing property','{"n":2}',false],['extra property','{"n":2,"tags":["x"],"secret":"sk"}',false],['wrong type','{"n":"two","tags":["x"]}',false],
    ['huge exponent','{"n":1e999999,"tags":["x"]}',false],['huge negative exponent','{"n":1e-401,"tags":["x"]}',false],['long literal',`{"n":${'7'.repeat(257)},"tags":["x"]}`,false],
  ])('validates %s without including values in errors', (_name,text,isValid) => {
    const structured = {...request(),structuredOutput:{name:'count',schema}};
    if (isValid) expect(extractStructuredAnswer(structured,text)).toBe(text.startsWith('```') ? '{"n":2,"tags":["x"]}' : text);
    else { expect(() => extractStructuredAnswer(structured,text)).toThrow(); try { extractStructuredAnswer(structured,text); } catch (error) { expect(String(error)).not.toContain('sk'); } }
  });
});
describe('Go query configuration scenarios', () => {
  const base = (): TextGenerationQueryConfig => ({ definition:{operation:{connectorId:'test-lab',operationId:'generateText'},branches:textGenerationBranchDefinitions(),stepDefaults:{executeDurability:'sync',executeMethodTimeoutMs:60000}},wireFormat:wireFormat(),baseURL:'https://api.example.test/v1',connectionModel:'test-model',requestTimeoutMs:10000,resolveCredential:()=>'test-key',maxResponseBytes:65536,maxStreamEventBytes:4096 });
  const cases: [string,(config:TextGenerationQueryConfig)=>void][] = [
    ['operation ID',config => { config.definition.operation.operationId = 'generateContent'; }],
    ['missing branch',config => { config.definition.branches = config.definition.branches.slice(0,5); }],
    ['required optional branch',config => { config.definition.branches = textGenerationBranchDefinitions(); config.definition.branches[1]!.optional = false; }],
    ['no encoder',config => { config.wireFormat.encodeRequest = undefined as never; }],
    ['credential header',config => { config.wireFormat.credentialHeader.name = 'Bad Header'; }],
    ['finish mapping',config => { config.wireFormat.finishReasons.odd = 'maybe' as never; }],
    ['error rule outcome',config => { config.wireFormat.errorRules = [{statusCode:400,outcome:undefined as never}]; }],
    ['error rule status',config => { config.wireFormat.errorRules = [{statusCode:200,outcome:{disposition:'retry',kind:'AVAILABILITY'}}]; }],
    ['error rule token',config => { config.wireFormat.errorRules = [{statusCode:0,errorToken:'has space',outcome:{disposition:'retry',kind:'AVAILABILITY'}}]; }],
    ['plain HTTP base URL',config => { config.baseURL = 'http://api.example.test/v1'; }],
    ['base URL user info',config => { config.baseURL = 'https://user:hunter2@api.example.test'; }],
    ['model ID rule',config => { config.wireFormat.modelIDRule = 'invalid' as never; }],
    ['connection model',config => { config.connectionModel = 'has space'; }],
    ['request timeout',config => { config.requestTimeoutMs = 0; }],
    ['credential resolver',config => { config.resolveCredential = undefined as never; }],
    ['response limit',config => { config.maxResponseBytes = 0; }],
    ['stream event limit',config => { config.maxStreamEventBytes = 0; }],
    ['negative stall',config => { config.wireFormat.stallTimeoutMs = -1; }],
    ['rate-limit header',config => { config.wireFormat.rateLimitHeaders = ['bad header']; }],
    ['error-token pointer',config => { config.wireFormat.errorTokenPointers = ['error/code']; }],
    ['missing provider name',config => { config.wireFormat.providerName = ''; }],
    ['async durability',config => { config.definition.stepDefaults.executeDurability = 'async'; }],
    ['flow-default durability',config => { config.definition.stepDefaults.executeDurability = 'default'; }],
    ['heartbeat timeout below two beats',config => { config.definition.stepDefaults.heartbeatTimeoutMs = 9000; }],
    ['Execute timeout within the request timeout',config => { config.definition.stepDefaults.executeMethodTimeoutMs = config.requestTimeoutMs; }],
  ];
  it.each(cases)('rejects %s',(_name,configure) => {
    const config = base(); configure(config);
    expect(() => newTextGenerationQuery(config)).toThrow();
    try { newTextGenerationQuery(config); } catch (error) { expect(String(error)).not.toContain('hunter2'); }
  });
  it('accepts zero and ten-second heartbeat defaults', () => {
    for (const heartbeatTimeoutMs of [0,10000]) { const config = base(); config.definition.stepDefaults.heartbeatTimeoutMs = heartbeatTimeoutMs; expect(() => newTextGenerationQuery(config)).not.toThrow(); }
  });
});
