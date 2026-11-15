// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * Nanosecond UTC times parsed and formatted as Go's time package does for the recordChanged Trigger. A time
 * is a bigint of nanoseconds since the Unix epoch, so cursor tokens and event IDs match Go's output byte for
 * byte beyond Date's millisecond precision.
 */

export const NanosecondsPerMillisecond = 1_000_000n;
export const NanosecondsPerSecond = 1_000_000_000n;
const nanosecondsPerDay = 86_400n * NanosecondsPerSecond;
const rfc3339Pattern = /^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?(Z|[+-][0-9]{2}:[0-9]{2})$/;

/**
 * Parse an RFC 3339 time as Go's time.Parse(time.RFC3339Nano) does; undefined when it is invalid. Fractional
 * digits past the ninth are dropped, as Go drops them.
 */
export function parseRFC3339Nanoseconds(text: string): bigint | undefined {
  const match = rfc3339Pattern.exec(text);
  if (!match) return undefined;
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month)) return undefined;
  if (hour > 23 || minute > 59 || second > 59) return undefined;
  const fraction = (match[7] ?? '').slice(0, 9).padEnd(9, '0');
  const zone = match[8]!;
  let offsetSeconds = 0;
  if (zone !== 'Z') {
    const zoneHours = Number(zone.slice(1, 3)), zoneMinutes = Number(zone.slice(4, 6));
    if (zoneHours > 23 || zoneMinutes > 59) return undefined;
    offsetSeconds = (zoneHours * 3600 + zoneMinutes * 60) * (zone[0] === '-' ? -1 : 1);
  }
  const seconds = BigInt(daysFromCivil(year, month, day)) * 86_400n + BigInt(hour * 3600 + minute * 60 + second - offsetSeconds);
  return seconds * NanosecondsPerSecond + BigInt(fraction);
}

/** Format a time as Go's time.Time.UTC().Format(time.RFC3339Nano): trailing fraction zeros dropped. */
export function formatRFC3339Nanoseconds(at: bigint): string {
  const { date, nanoseconds } = split(at);
  const fraction = nanoseconds.toString().padStart(9, '0').replace(/0+$/, '');
  return `${date}${fraction ? `.${fraction}` : ''}Z`;
}

/** Format a time as Go's Format("2006-01-02T15:04:05.000Z"), truncating to milliseconds. */
export function formatMilliseconds(at: bigint): string {
  const { date, nanoseconds } = split(at);
  return `${date}.${(nanoseconds / NanosecondsPerMillisecond).toString().padStart(3, '0')}Z`;
}

/** Format a time as Go's Format("20060102T150405.000000000Z") with the period removed. */
export function formatEventIDTime(at: bigint): string {
  const { date, nanoseconds } = split(at);
  return `${date.replace(/[-:]/g, '')}${nanoseconds.toString().padStart(9, '0')}Z`;
}

/** Round a time down to its millisecond, as Go's Truncate(time.Millisecond). */
export function truncateToMillisecond(at: bigint): bigint {
  return at - floorModulo(at, NanosecondsPerMillisecond);
}

/** Convert to a Date, dropping sub-millisecond digits. */
export function nanosecondsToDate(at: bigint): Date {
  return new Date(Number(floorDivide(at, NanosecondsPerMillisecond)));
}

function split(at: bigint): { date: string; nanoseconds: bigint } {
  const days = floorDivide(at, nanosecondsPerDay);
  const withinDay = at - days * nanosecondsPerDay;
  const [year, month, day] = civilFromDays(Number(days));
  const seconds = Number(withinDay / NanosecondsPerSecond);
  const nanoseconds = withinDay % NanosecondsPerSecond;
  const two = (value: number) => value.toString().padStart(2, '0');
  const date = `${year.toString().padStart(4, '0')}-${two(month)}-${two(day)}T${two(Math.floor(seconds / 3600))}:${two(Math.floor(seconds / 60) % 60)}:${two(seconds % 60)}`;
  return { date, nanoseconds };
}

function floorDivide(value: bigint, divisor: bigint): bigint {
  const quotient = value / divisor;
  return value % divisor < 0n ? quotient - 1n : quotient;
}

function floorModulo(value: bigint, divisor: bigint): bigint {
  return value - floorDivide(value, divisor) * divisor;
}

function daysInMonth(year: number, month: number): number {
  if (month === 2) return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28;
  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}

// Howard Hinnant's days_from_civil: days since 1970-01-01 in the proleptic Gregorian calendar.
function daysFromCivil(year: number, month: number, day: number): number {
  const shiftedYear = month <= 2 ? year - 1 : year;
  const era = Math.floor(shiftedYear / 400);
  const yearOfEra = shiftedYear - era * 400;
  const dayOfYear = Math.floor((153 * (month + (month > 2 ? -3 : 9)) + 2) / 5) + day - 1;
  const dayOfEra = yearOfEra * 365 + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100) + dayOfYear;
  return era * 146_097 + dayOfEra - 719_468;
}

function civilFromDays(daysSinceEpoch: number): [number, number, number] {
  const days = daysSinceEpoch + 719_468;
  const era = Math.floor(days / 146_097);
  const dayOfEra = days - era * 146_097;
  const yearOfEra = Math.floor((dayOfEra - Math.floor(dayOfEra / 1460) + Math.floor(dayOfEra / 36_524) - Math.floor(dayOfEra / 146_096)) / 365);
  const dayOfYear = dayOfEra - (365 * yearOfEra + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100));
  const monthIndex = Math.floor((5 * dayOfYear + 2) / 153);
  const day = dayOfYear - Math.floor((153 * monthIndex + 2) / 5) + 1;
  const month = monthIndex + (monthIndex < 10 ? 3 : -9);
  return [yearOfEra + era * 400 + (month <= 2 ? 1 : 0), month, day];
}
