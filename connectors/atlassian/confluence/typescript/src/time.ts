// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** Parsed Go RFC3339Nano value with a canonical JSON timestamp and UTC milliseconds. */
export interface ParsedGoTime { canonical: string; epochMs: number; }

/** Parse Go's RFC3339Nano calendar and zone rules without Date.parse normalization. */
export function parseGoRFC3339(value: string): ParsedGoTime | undefined {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})$/.exec(value);
  if (!match) return undefined;
  const year = Number(match[1]), month = Number(match[2]), day = Number(match[3]);
  const hour = Number(match[4]), minute = Number(match[5]), second = Number(match[6]);
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) || hour > 23 || minute > 59 || second > 59) return undefined;
  const zone = match[8]!;
  const zoneHours = zone === 'Z' ? 0 : Number(zone.slice(1, 3));
  const zoneMinutes = zone === 'Z' ? 0 : Number(zone.slice(4, 6));
  if (zoneMinutes > 59) return undefined;
  const offsetMinutes = (zone.startsWith('-') ? -1 : 1) * (zoneHours * 60 + zoneMinutes);
  const fraction = (match[7] || '').slice(0, 9).padEnd(9, '0');
  const calendar = new Date(0);
  calendar.setUTCFullYear(year, month - 1, day);
  calendar.setUTCHours(hour, minute, second, Number(fraction.slice(0, 3)));
  const epochMs = calendar.getTime() - offsetMinutes * 60_000;
  if (!Number.isFinite(epochMs)) return undefined;
  const canonicalFraction = fraction.replace(/0+$/, '');
  const canonicalZone = zone === 'Z' || offsetMinutes === 0 ? 'Z' : zone;
  const canonical = value.slice(0, 19) + (canonicalFraction ? `.${canonicalFraction}` : '') + canonicalZone;
  return { canonical, epochMs };
}
function daysInMonth(year: number, month: number): number {
  if ([4, 6, 9, 11].includes(month)) return 30;
  if (month === 2) return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28;
  return 31;
}
