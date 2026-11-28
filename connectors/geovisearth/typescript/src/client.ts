// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { SecretString, newQueryBranch, newQueryRetry, type Call, type CredentialProvider, type Failure, type Query, type QueryAttempt, type QueryDefinition } from '@superdurable/connectors-sdk';
import { BodyTooLargeError, parseRetryAfter, sendProviderRequest, type ProviderRequest, type ProviderResponse } from '@superdurable/connectors-sdk/providerhttp';
import { GetCurrentWeatherDefinition } from './generated/definitions.js';
import type { Config, Credentials, GetCurrentWeatherInput, GetCurrentWeatherOutput } from './generated/models.js';

/** Optional HTTP transport for provider tests. */
export interface GeovisearthClientOptions { send?: (request: ProviderRequest) => Promise<ProviderResponse>; }
const emptyWeather: GetCurrentWeatherOutput = { areaCode: '', weather: '', weatherCode: '', temperatureCelsius: 0, relativeHumidityPercent: 0, windDirection: '', windForce: '', surfacePressureHpa: 0, observedAt: '' };

/** Resolve GEOVIS Earth credentials for each weather lookup. */
export class GeovisearthClient {
  private readonly baseUrl: string;
  private readonly timeoutMs: number;
  private readonly maxResponseBytes: number;
  private readonly send: (request: ProviderRequest) => Promise<ProviderResponse>;

  /** Validate connection settings without contacting the provider. */
  constructor(config: Config, private readonly credentials: CredentialProvider<Credentials>, options: GeovisearthClientOptions = {}) {
    if (!config || !credentials) throw new TypeError('GEOVIS Earth configuration and credentials are required');
    const origin = (config.baseUrl || 'https://api.open.geovisearth.com').replace(/\/+$/, '');
    if (origin !== 'https://api.open.geovisearth.com') throw new TypeError('GEOVIS Earth origin is invalid');
    this.baseUrl = origin;
    this.timeoutMs = config.timeoutMs || 10_000;
    this.maxResponseBytes = config.maxResponseBytes || 262_144;
    if (!Number.isSafeInteger(this.timeoutMs) || this.timeoutMs <= 0 || !Number.isSafeInteger(this.maxResponseBytes) || this.maxResponseBytes <= 0) throw new TypeError('GEOVIS Earth request limits are invalid');
    this.send = options.send ?? sendProviderRequest;
  }
  /** Bind the current-weather Query. */ getCurrentWeather(): GetCurrentWeatherOperation { return new GetCurrentWeatherOperation(this); }

  /** Send one bounded weather request without exposing its query-string token. */
  async fetchWeather(call: Call, location: string): Promise<{ response?: ProviderResponse; attempt?: QueryAttempt<GetCurrentWeatherOutput>; error?: 'credential' | 'transport' | 'tooLarge'; retryAfterMs: number }> {
    let token = '';
    try { const resolved = await this.credentials.resolve(call, call.context.cancellationSignal); token = resolved.token instanceof SecretString ? resolved.token.reveal() : ''; }
    catch { return { error: 'credential', retryAfterMs: 0 }; }
    if (!token || token.length > 512 || /[^\x21-\x7e]/.test(token)) return { error: 'credential', retryAfterMs: 0 };
    const url = new URL('/v2/cn/area/basic', this.baseUrl);
    url.searchParams.set('location', location);
    url.searchParams.set('token', token);
    const request: ProviderRequest = { url: url.toString(), method: 'GET', headers: { Accept: 'application/json' }, timeoutMs: this.timeoutMs, maxResponseBytes: this.maxResponseBytes, addressPolicy: 'publicHTTPS', signal: call.context.cancellationSignal };
    let response: ProviderResponse;
    try { response = await this.send(request); }
    catch (error) {
      if (error instanceof BodyTooLargeError && error.status !== undefined) return { response: { status: error.status, headers: error.headers ?? {}, body: new Uint8Array() }, error: 'tooLarge', retryAfterMs: parseRetryAfter(header(error.headers ?? {},'retry-after')) };
      return { error: 'transport', retryAfterMs: 0 };
    }
    const retryAfterMs = parseRetryAfter(header(response.headers, 'retry-after'));
    if (response.body.length > this.maxResponseBytes) return { response, error: 'tooLarge', retryAfterMs };
    if (response.status === 200) return { response, attempt: decodeObservation(response.body, token), retryAfterMs };
    return { response, retryAfterMs };
  }
}

/** Get one current-weather observation through GEOVIS Earth. */
export class GetCurrentWeatherOperation implements Query<GetCurrentWeatherInput, GetCurrentWeatherOutput> {
  /** Bind the provider client. */ constructor(private readonly client: GeovisearthClient) {}
  /** Return the generated Query definition. */ definition(): QueryDefinition { return GetCurrentWeatherDefinition; }
  /** Classify one provider response with the Go connector contract. */
  async invoke(call: Call, input: GetCurrentWeatherInput): Promise<QueryAttempt<GetCurrentWeatherOutput>> {
    if (typeof input?.location !== 'string' || !isValidLocation(input.location)) return branch('defect', 'VALIDATION');
    const exchange = await this.client.fetchWeather(call, input.location);
    if (exchange.error === 'credential') return branch('defect', 'AUTHENTICATION');
    if (exchange.error === 'transport' || !exchange.response) return retry('AVAILABILITY');
    const { status } = exchange.response;
    if (status === 429) return retry('RATE_LIMIT', exchange.retryAfterMs);
    if (status === 408 || status >= 500) return retry('AVAILABILITY', exchange.retryAfterMs);
    if (exchange.error === 'tooLarge') return branch('invalidResponse', 'RESPONSE_TOO_LARGE');
    if (status !== 200) return branch('providerRejected', status === 401 ? 'AUTHENTICATION' : status === 403 ? 'AUTHORIZATION' : 'PROVIDER_REJECTION');
    return exchange.attempt ?? invalidWeather();
  }
}

function invalidWeather(): QueryAttempt<GetCurrentWeatherOutput> { return branch('invalidResponse', 'PROTOCOL'); }

function fields(value: unknown): Record<string, unknown> | undefined {
  if (value == null) return {};
  return typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined;
}
function decodeObservation(body: Uint8Array, token: string): QueryAttempt<GetCurrentWeatherOutput> {
  let document: unknown;
  try {
    document = JSON.parse(new TextDecoder('utf-8', {ignoreBOM:true}).decode(body), (key: string,value:unknown,context?:{source?:string}) => {
      if (value !== null && typeof value==='object' && !Array.isArray(value))
        return Object.fromEntries(Object.entries(value).map(([name,field])=>[name.toLowerCase(),field]));
      return readProviderNumber(key,value,context);
    });
  } catch { return invalidWeather(); }
  const data=fields(document);
  if (!data || typeof data.status !== 'bigint') return invalidWeather();
  const result=fields(data.result),date=fields(data.date),location=fields(data.location);
  if (!result || !date || !location) return invalidWeather();
  const textFields=[result.wp,result.wp_code,result.wd_desc,result.ws_desc,date.time,location.areacode];
  if (textFields.some(field=>field!=null && typeof field!=='string')
    || (result.rh!=null && typeof result.rh!=='bigint')
    || [result.tem,result.prs].some(value=>value!=null && !Number.isFinite(value))) return invalidWeather();
  if (data.status!==0n) return branch('providerRejected','PROVIDER_REJECTION');
  const {wp,wp_code,wd_desc,ws_desc,rh,prs}=result;
  if (typeof wp!=='string' || !wp || typeof wp_code!=='string' || !wp_code || typeof wd_desc!=='string' || !wd_desc
    || typeof ws_desc!=='string' || !ws_desc || typeof rh!=='bigint' || rh<0n || rh>100n
    || typeof prs!=='number' || prs<=0 || typeof date.time!=='string' || !isValidTimestamp(date.time)
    || textFields.some(field=>typeof field==='string' && field.includes(token)))return invalidWeather();
  return newQueryBranch('fetched',{areaCode:typeof location.areacode==='string'?location.areacode:'',weather:wp,weatherCode:wp_code,
    temperatureCelsius:typeof result.tem==='number'?result.tem:0,relativeHumidityPercent:Number(rh),windDirection:wd_desc,windForce:ws_desc,surfacePressureHpa:prs,observedAt:date.time});
}

// Go decodes status and relative humidity into signed integers before classifying a response.
function readProviderNumber(key: string, value: unknown, context?: { source?: string }): unknown {
  if (!['status', 'rh'].includes(key.toLowerCase()) || typeof value !== 'number') return value;
  const source = context?.source;
  if (source === undefined) return Number.isSafeInteger(value) ? BigInt(value) : false;
  if (!/^-?\d+$/.test(source) || source.length > 20) return false;
  const integer = BigInt(source);
  return integer >= -(1n << 63n) && integer < (1n << 63n) ? integer : false;
}
function isValidLocation(location:string):boolean {
  if (/^WTX_CH[0-9]{9}$/.test(location)) return true;
  const [lon,lat,...extra]=location.split(',');
  return extra.length===0 && lon!==undefined && lat!==undefined && validCoordinate(lon,180) && validCoordinate(lat,90);
}
function isValidTimestamp(value:string):boolean{
  if(!/^[0-9]{14}$/.test(value))return false;
  const year=Number(value.slice(0,4)),month=Number(value.slice(4,6)),day=Number(value.slice(6,8));
  const days=[31,year%4===0 && (year%100!==0 || year%400===0)?29:28,31,30,31,30,31,31,30,31,30,31];
  return day>=1 && day<=(days[month-1]??0) && Number(value.slice(8,10))<24 && Number(value.slice(10,12))<60 && Number(value.slice(12,14))<60;
}
function validCoordinate(text: string, maximum: number): boolean {
  const pattern = /^[+-]?(?:(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[Ee][+-]?[0-9](?:_?[0-9])*)?|0[xX](?:_?[0-9A-Fa-f](?:_?[0-9A-Fa-f])*(?:\.(?:[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)?)?|\.[0-9A-Fa-f](?:_?[0-9A-Fa-f])*)[pP][+-]?[0-9](?:_?[0-9])*)$/;
  if (!pattern.test(text)) return false;
  const literal = text.replace(/^[+-]/, '').replaceAll('_', '');
  if (!/^0x/i.test(literal)) return Number(literal) <= maximum;
  const [mantissa = '', power = ''] = literal.slice(2).split(/[pP]/);
  const point = mantissa.indexOf('.');
  const integerLength = point < 0 ? mantissa.length : point;
  const digits = mantissa.replace('.', '');
  const significant = digits.replace(/^0+/, '');
  if (!significant) return true;
  const prefix = significant.slice(0, 16);
  let head = BigInt('0x' + prefix);
  // Preserve the rounding direction when discarded hexadecimal digits are nonzero.
  if (significant.length > prefix.length && /[1-9a-f]/i.test(significant.slice(prefix.length))) head |= 1n;
  const scale = Number(power) + 4 * (integerLength - (digits.length-significant.length) - prefix.length);
  return Number(head) * 2 ** scale <= maximum;
}
function header(headers: ProviderResponse['headers'], name: string): string { const value = Object.entries(headers).find(([key]) => key.toLowerCase() === name)?.[1]; return Array.isArray(value) ? value[0] ?? '' : value ?? ''; }
function failure(kind: Failure['kind']): Failure { return { provider: 'geovisearth', operation: 'getCurrentWeather', kind, message: 'GEOVIS Earth weather query failed' }; }
function branch(name: string, kind: Failure['kind']): QueryAttempt<GetCurrentWeatherOutput> { return newQueryBranch(name, emptyWeather, {}, failure(kind)); }
function retry(kind: Failure['kind'], delay = 0): QueryAttempt<GetCurrentWeatherOutput> { return newQueryRetry(failure(kind), delay); }
