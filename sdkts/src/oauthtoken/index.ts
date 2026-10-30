// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createPrivateKey, sign, type KeyObject } from 'node:crypto';
import { ReauthorizationRequiredError } from '../errors.js';
import { SecretString } from '../credentials.js';

/** Longest early-refresh window in milliseconds. */
export const RefreshSkewMs = 300_000;

/** OAuth token endpoint client authentication. */
export type ClientAuthenticationMethod = 'client_secret_post' | 'client_secret_basic' | 'none';

/** OAuth client material kept outside durable Flow state. */
export interface ClientCredentials {
  /** Public client identifier. */ id: string;
  /** Client secret, omitted for a public client. */ secret?: SecretString;
  /** RFC 7591 token endpoint authentication method. */ authenticationMethod: ClientAuthenticationMethod;
}

/** Successful replacement material from one OAuth grant. */
export class TokenResponse {
  /** Replacement access token. */ readonly accessToken: SecretString;
  /** Rotated refresh token, empty when the provider retained it. */ readonly refreshToken: SecretString;
  /** Access lifetime in milliseconds; zero only when explicitly accepted. */ readonly expiresInMs: number;
  /** Refresh token lifetime in milliseconds; zero when omitted. */ readonly refreshTokenExpiresInMs: number;
  /** Returned scope string, possibly empty. */ readonly scope: string;
  /** Returned token type, possibly empty when explicitly accepted. */ readonly tokenType: string;
  /** Only explicitly retained non-standard response fields. */ readonly retainedFields: Readonly<Record<string, unknown>>;

  /** Capture validated provider response fields. */
  constructor(fields: { accessToken: SecretString; refreshToken: SecretString; expiresInMs: number; refreshTokenExpiresInMs: number; scope: string; tokenType: string; retainedFields: Record<string, unknown> }) {
    this.accessToken = fields.accessToken;
    this.refreshToken = fields.refreshToken;
    this.expiresInMs = fields.expiresInMs;
    this.refreshTokenExpiresInMs = fields.refreshTokenExpiresInMs;
    this.scope = fields.scope;
    this.tokenType = fields.tokenType;
    this.retainedFields = fields.retainedFields;
  }

  /** Keep the previous refresh token when no rotated value was returned. */
  nextRefreshToken(prior: SecretString): SecretString { return this.refreshToken.reveal() ? this.refreshToken : prior; }
}

/** One provider token endpoint and its declared response rules. */
export interface TokenEndpointOptions {
  /** Safe provider name used in errors. */ providerName: string;
  /** Absolute HTTPS URL, or HTTP only when explicitly allowed. */ url: string;
  /** Provider error codes that require interactive reauthorization below HTTP 500. */ terminalErrorCodes?: readonly string[];
  /** Accepted token types, case insensitive; defaults to Bearer. */ acceptedTokenTypes?: readonly string[];
  /** Accept a successful response without token_type. */ acceptsMissingTokenType?: boolean;
  /** Accept a successful response without expires_in. */ acceptsMissingExpiresIn?: boolean;
  /** Non-standard, non-secret top-level fields to retain. */ retainedResponseFields?: readonly string[];
  /** Send JSON instead of form encoding when the provider requires it. */ usesJSONRequestBody?: boolean;
  /** Permit a declared HTTP endpoint, including private-network identity servers. */ allowsPlainHTTP?: boolean;
  /** Fixed non-secret grant parameters that cannot override OAuth grant fields. */ additionalRequestParameters?: Readonly<Record<string, string>>;
  /** Caller-owned fetch implementation for a proxy or local test. */ fetch?: typeof fetch;
}

/** Provider-neutral OAuth refresh and client-credentials exchanges. */
export class TokenEndpoint {
  readonly #options: TokenEndpointOptions;

  /** Capture endpoint rules without sending a request. */
  constructor(options: TokenEndpointOptions) { this.#options = options; }

  /** Exchange a refresh token, preserving terminal-error classification. */
  async exchangeRefreshToken(client: ClientCredentials, refreshToken: SecretString, signal?: AbortSignal): Promise<TokenResponse> {
    if (!client.id || !refreshToken.reveal()) throw new ReauthorizationRequiredError();
    const parameters: Record<string, string> = { grant_type: 'refresh_token', refresh_token: refreshToken.reveal() };
    const basicClient = this.#addClientAuthentication(parameters, client);
    return this.#exchange(parameters, basicClient, signal);
  }

  /** Obtain an access token with the OAuth client-credentials grant. */
  async exchangeClientCredentials(client: ClientCredentials, scopes: readonly string[] = [], signal?: AbortSignal): Promise<TokenResponse> {
    if (!client.id || !client.secret?.reveal()) throw new ReauthorizationRequiredError();
    if (client.authenticationMethod === 'none') throw new TypeError(`${this.#options.providerName} client credentials grant needs a confidential client`);
    const parameters: Record<string, string> = { grant_type: 'client_credentials' };
    if (scopes.length) parameters.scope = scopes.join(' ');
    const basicClient = this.#addClientAuthentication(parameters, client);
    return this.#exchange(parameters, basicClient, signal);
  }

  /** Exchange a signed JWT bearer assertion without client credentials. */
  async exchangeJWTBearerAssertion(assertion: SecretString, signal?: AbortSignal): Promise<TokenResponse> {
    if (!assertion.reveal()) throw new ReauthorizationRequiredError();
    return this.#exchange({ grant_type: 'urn:ietf:params:oauth:grant-type:jwt-bearer', assertion: assertion.reveal() }, undefined, signal);
  }

  #addClientAuthentication(parameters: Record<string, string>, client: ClientCredentials): ClientCredentials | undefined {
    switch (client.authenticationMethod) {
      case 'client_secret_post':
        if (!client.secret?.reveal()) throw new ReauthorizationRequiredError();
        parameters.client_id = client.id;
        parameters.client_secret = client.secret.reveal();
        return undefined;
      case 'client_secret_basic':
        if (!client.secret?.reveal()) throw new ReauthorizationRequiredError();
        return client;
      case 'none':
        parameters.client_id = client.id;
        return undefined;
      default: throw new TypeError(`${this.#options.providerName} OAuth client authentication method is not supported`);
    }
  }

  async #exchange(parameters: Record<string, string>, basicClient: ClientCredentials | undefined, signal: AbortSignal | undefined): Promise<TokenResponse> {
    const options = this.#options;
    validateEndpointURL(options.url, options.allowsPlainHTTP ?? false, options.providerName);
    for (const field of options.retainedResponseFields ?? []) {
      if (standardResponseFields.has(field)) throw new TypeError(`${options.providerName} token endpoint cannot retain the standard ${field} field`);
    }
    for (const [parameter, value] of Object.entries(options.additionalRequestParameters ?? {})) {
      if (grantParameters.has(parameter)) throw new TypeError(`${options.providerName} token endpoint cannot replace the ${parameter} request parameter`);
      parameters[parameter] = value;
    }
    const headers: Record<string, string> = { Accept: 'application/json' };
    let body: string;
    if (options.usesJSONRequestBody) {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(parameters);
    } else {
      headers['Content-Type'] = 'application/x-www-form-urlencoded';
      body = new URLSearchParams(parameters).toString();
    }
    if (basicClient) headers.Authorization = `Basic ${Buffer.from(`${basicClient.id}:${basicClient.secret?.reveal() ?? ''}`).toString('base64')}`;
    let response: Response;
    try {
      response = await (options.fetch ?? fetch)(options.url, { method: 'POST', headers, body, redirect: 'manual', ...(signal ? { signal } : {}) });
    } catch {
      throw new Error(`${options.providerName} token endpoint is unavailable`);
    }
    let contents: Uint8Array;
    try { contents = await readBoundedBody(response, 1 << 20); } catch { throw new Error(`${options.providerName} token response could not be read`); }
    let decoded: Record<string, unknown>;
    try {
      const value: unknown = JSON.parse(new TextDecoder().decode(contents));
      if (!value || typeof value !== 'object' || Array.isArray(value)) throw new TypeError();
      decoded = value as Record<string, unknown>;
    } catch { throw new Error(`${options.providerName} token response is invalid`); }
    const errorCode = typeof decoded.error === 'string' ? decoded.error : '';
    if (!response.ok || errorCode) {
      if (response.status < 500 && errorCode && options.terminalErrorCodes?.includes(errorCode)) throw new ReauthorizationRequiredError();
      throw new Error(`${options.providerName} token endpoint returned HTTP ${response.status}`);
    }
    const accessToken = decoded.access_token;
    const expiresIn = decoded.expires_in;
    const tokenType = decoded.token_type;
    const hasAcceptedExpiry = Number.isSafeInteger(expiresIn) && (expiresIn as number) > 0 || ((expiresIn === undefined || expiresIn === 0) && options.acceptsMissingExpiresIn === true);
    const acceptedTypes = options.acceptedTokenTypes?.length ? options.acceptedTokenTypes : ['Bearer'];
    const hasAcceptedType = typeof tokenType === 'string' && acceptedTypes.some(accepted => accepted.toLowerCase() === tokenType.toLowerCase()) || ((tokenType === undefined || tokenType === '') && options.acceptsMissingTokenType === true);
    if (typeof accessToken !== 'string' || !accessToken || !hasAcceptedExpiry || !hasAcceptedType) throw new Error(`${options.providerName} token response omitted required fields`);
    const retainedFields: Record<string, unknown> = {};
    for (const field of options.retainedResponseFields ?? []) if (Object.hasOwn(decoded, field)) retainedFields[field] = decoded[field];
    const refreshToken = typeof decoded.refresh_token === 'string' ? decoded.refresh_token : '';
    const refreshTokenExpiry = typeof decoded.refresh_token_expires_in === 'number' && decoded.refresh_token_expires_in > 0 ? decoded.refresh_token_expires_in : 0;
    return new TokenResponse({ accessToken: new SecretString(accessToken), refreshToken: new SecretString(refreshToken), expiresInMs: (expiresIn as number | undefined ?? 0) * 1000, refreshTokenExpiresInMs: refreshTokenExpiry * 1000, scope: typeof decoded.scope === 'string' ? decoded.scope : '', tokenType: typeof tokenType === 'string' ? tokenType : '', retainedFields });
  }
}

const grantParameters = new Set(['grant_type', 'refresh_token', 'client_id', 'client_secret', 'assertion', 'client_assertion', 'client_assertion_type']);
const standardResponseFields = new Set(['access_token', 'refresh_token', 'expires_in', 'refresh_token_expires_in', 'scope', 'token_type', 'error', 'error_description', 'error_uri', 'id_token']);

function validateEndpointURL(value: string, allowsPlainHTTP: boolean, providerName: string): void {
  let parsed: URL;
  try { parsed = new URL(value); } catch { throw new TypeError(`${providerName} token endpoint is invalid`); }
  if (!parsed.hostname || (parsed.protocol !== 'https:' && !(allowsPlainHTTP && parsed.protocol === 'http:')) || parsed.username || parsed.password) throw new TypeError(`${providerName} token endpoint is invalid`);
}

async function readBoundedBody(response: Response, maximumBytes: number): Promise<Uint8Array> {
  if (!response.body) return new Uint8Array();
  const reader = response.body.getReader();
  const chunks: Uint8Array[] = [];
  let length = 0;
  try {
    for (;;) {
      const result = await reader.read();
      if (result.done) break;
      length += result.value.byteLength;
      if (length > maximumBytes) throw new RangeError('token response too large');
      chunks.push(result.value);
    }
  } finally { reader.releaseLock(); }
  const contents = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) { contents.set(chunk, offset); offset += chunk.byteLength; }
  return contents;
}

/** Return min(five minutes, one quarter of a known positive token lifetime). */
export function proportionalRefreshSkewMs(issuedAt?: Date, expiresAt?: Date): number { return lifetimeShare(RefreshSkewMs, issuedAt, expiresAt); }

/** Return min(one minute, one quarter of a known positive token lifetime). */
export function forcedRefreshFloorMs(issuedAt?: Date, expiresAt?: Date): number { return lifetimeShare(60_000, issuedAt, expiresAt); }

/** Decide whether an absent or soon-expiring access token needs refresh. */
export function isRefreshRequired(hasAccessToken: boolean, expiresAt: Date | undefined, now: Date, missingExpiryRule: 'refresh' | 'keep', issuedAt?: Date): boolean {
  if (!hasAccessToken) return true;
  if (!expiresAt) return missingExpiryRule !== 'keep';
  return expiresAt.getTime() <= now.getTime() + proportionalRefreshSkewMs(issuedAt, expiresAt);
}

/** Decide with issue time so short-lived tokens use proportional skew. */
export function isRefreshRequiredWithIssueTime(hasAccessToken: boolean, issuedAt: Date | undefined, expiresAt: Date | undefined, now: Date, missingExpiryRule: 'refresh' | 'keep'): boolean {
  return isRefreshRequired(hasAccessToken, expiresAt, now, missingExpiryRule, issuedAt);
}

/** Check whether all required scopes appear in a returned space- or comma-separated set. */
export function hasAllScopes(returnedScope: string, requiredScopes: readonly string[]): boolean {
  const granted = new Set(returnedScope.split(/[ ,]+/).filter(Boolean));
  return requiredScopes.every(scope => granted.has(scope));
}

/** Check set equality of returned and required scopes. */
export function hasExactScopes(returnedScope: string, requiredScopes: readonly string[]): boolean {
  const granted = new Set(returnedScope.split(/[ ,]+/).filter(Boolean));
  return granted.size === new Set(requiredScopes).size && hasAllScopes(returnedScope, requiredScopes);
}

function lifetimeShare(maximumMs: number, issuedAt?: Date, expiresAt?: Date): number {
  const lifetimeMs = issuedAt && expiresAt ? expiresAt.getTime() - issuedAt.getTime() : 0;
  return lifetimeMs > 0 ? Math.min(maximumMs, lifetimeMs / 4) : maximumMs;
}

/** Claims of an RS256 JWT bearer assertion. */
export interface JWTBearerAssertion {
  /** iss claim. */ issuer: string;
  /** Optional sub claim. */ subject?: string;
  /** aud claim, usually the token URL. */ audience: string;
  /** Optional scope claim. */ scope?: string;
  /** iat claim. */ issuedAt: Date;
  /** Positive lifetime in milliseconds. */ lifetimeMs: number;
}

/** Parse a PKCS #1 or PKCS #8 RSA PEM key without exposing key material in errors. */
export function parseRSAPrivateKeyPEM(pemText: string): KeyObject {
  try {
    const key = createPrivateKey(pemText);
    if (key.asymmetricKeyType !== 'rsa') throw new TypeError();
    return key;
  } catch { throw new TypeError('private key is invalid or must use RSA'); }
}

/** Sign a JWT bearer grant assertion with RS256. */
export function signJWTBearerAssertion(assertion: JWTBearerAssertion, privateKey: KeyObject): SecretString {
  if (!privateKey || privateKey.asymmetricKeyType !== 'rsa') throw new TypeError('JWT-bearer assertion private key is missing or invalid');
  if (!assertion.issuer || !assertion.audience || !Number.isFinite(assertion.issuedAt.getTime()) || !Number.isSafeInteger(assertion.lifetimeMs) || assertion.lifetimeMs <= 0) throw new TypeError('JWT-bearer assertion claims are incomplete');
  const claims: Record<string, string | number> = { aud: assertion.audience, exp: Math.floor((assertion.issuedAt.getTime() + assertion.lifetimeMs) / 1000), iat: Math.floor(assertion.issuedAt.getTime() / 1000), iss: assertion.issuer };
  if (assertion.subject) claims.sub = assertion.subject;
  if (assertion.scope) claims.scope = assertion.scope;
  const unsigned = `${Buffer.from(JSON.stringify({ alg: 'RS256', typ: 'JWT' })).toString('base64url')}.${Buffer.from(JSON.stringify(claims)).toString('base64url')}`;
  const signature = sign('RSA-SHA256', Buffer.from(unsigned), privateKey).toString('base64url');
  return new SecretString(`${unsigned}.${signature}`);
}
