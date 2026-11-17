// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash, createHmac } from 'node:crypto';

/** SHA-256 of an empty ECS GET body. */
export const EmptyPayloadSHA256 = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855';
/** Canonical header order required by Alibaba Cloud signature V3. */
export const SignedHeaderNames = 'host;x-acs-action;x-acs-content-sha256;x-acs-date;x-acs-signature-nonce;x-acs-version';

/** Sign one ECS OpenAPI request with ACS3-HMAC-SHA256. */
export function signACS3Request(secret: string, method: string, host: string, action: string, canonicalQuery: string, requestDate: string, nonce: string): string {
  const canonicalHeaders = `host:${host}\n` +
    `x-acs-action:${action}\n` +
    `x-acs-content-sha256:${EmptyPayloadSHA256}\n` +
    `x-acs-date:${requestDate}\n` +
    `x-acs-signature-nonce:${nonce}\n` +
    'x-acs-version:2014-05-26\n';
  const canonicalRequest = `${method}\n/\n${canonicalQuery}\n${canonicalHeaders}\n${SignedHeaderNames}\n${EmptyPayloadSHA256}`;
  const digest = createHash('sha256').update(canonicalRequest, 'utf8').digest('hex');
  return createHmac('sha256', secret).update(`ACS3-HMAC-SHA256\n${digest}`, 'utf8').digest('hex');
}
