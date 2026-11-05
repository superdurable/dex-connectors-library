// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { z } from 'zod';

const GoTimeInputPattern = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/;

function isGoTimeInput(value: string): boolean {
  const parts = GoTimeInputPattern.exec(value);
  if (!parts || parts[0] !== value) return false;
  const year = Number(parts[1]);
  const month = Number(parts[2]);
  const day = Number(parts[3]);
  const hour = Number(parts[4]);
  const minute = Number(parts[5]);
  const second = Number(parts[6]);
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59) return false;
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (day < 1 || day > days[month - 1]!) return false;
  const offset = parts[8]!;
  return offset === 'Z' || (Number(offset.slice(1, 3)) <= 24 && Number(offset.slice(4, 6)) <= 60);
}

/** Go time.Time JSON input validator that preserves the exact timestamp string. */
export const GoTimeInputSchema = z.string().refine(isGoTimeInput, 'timestamp must be a Go RFC 3339 time');
