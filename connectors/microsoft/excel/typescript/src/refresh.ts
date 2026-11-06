// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { ManifestCredentialRefreshDriver } from '@superdurable/connectors-sdk/oauthtoken';
import { Authorization, CredentialFields } from './generated/refresh.js';
import type { Credentials } from './generated/models.js';

/** Refresh Microsoft OAuth credentials through the generated manifest declaration. */
export class ExcelCredentialRefreshDriver extends ManifestCredentialRefreshDriver<Credentials> {
  /** Bind the generated Microsoft OAuth grant and an optional test clock. */
  constructor(now: () => Date = () => new Date()) {
    super({ providerName: 'Microsoft', auth: Authorization, credentialFields: CredentialFields, now });
  }
}
