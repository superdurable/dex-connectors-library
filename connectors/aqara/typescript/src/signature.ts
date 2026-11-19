// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { createHash } from 'node:crypto';

/** Sign one Aqara v3 request using the provider's ordered header fields. */
export function signDeviceRequest(accessToken: string, appId: string, keyId: string, nonce: string, timestamp: string, appKey: string): string {
  const canonical = `Accesstoken=${accessToken}&Appid=${appId}&Keyid=${keyId}&Nonce=${nonce}&Time=${timestamp}${appKey}`.toLowerCase();
  return createHash('md5').update(canonical, 'utf8').digest('hex');
}
