// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { GmailMessagePart } from '../dist/index.js';

type PublishedMessagePart = {
  mimeType?: string | null;
  headers?: { name: string; value: string }[] | null;
  body?: { data?: string | null } | null;
  parts?: PublishedMessagePart[] | null;
};
type Assert<Value extends true> = Value;

export type PublishedOptionalFields = [
  Assert<[GmailMessagePart] extends [PublishedMessagePart] ? true : false>,
  Assert<[PublishedMessagePart] extends [GmailMessagePart] ? true : false>,
];
