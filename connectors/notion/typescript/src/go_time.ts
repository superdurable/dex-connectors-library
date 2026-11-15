// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/**
 * Go time.Time values with nanosecond precision and their fixed offset, parsed and formatted as Go does. The
 * databaseRowChanged cursor shares its bytes with the Go connector, so its times keep Go's precision, offset,
 * and RFC 3339 rendering, which Date cannot hold.
 */

const rfc3339 = /^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?(Z|[+-][0-9]{2}:[0-9]{2})$/;
const nanosecondsPerSecond = 1_000_000_000n;
const secondsPerDay = 86_400;

/** An instant in Unix nanoseconds and the fixed zone offset Go keeps for formatting. */
export class GoTime {
  /** Wrap an instant; offsetSeconds only changes how it is rendered. */
  constructor(readonly unixNanoseconds: bigint, readonly offsetSeconds = 0) {}

  /** Return the instant moved by nanoseconds, keeping the offset as Go's Time.Add does. */
  add(nanoseconds: bigint): GoTime { return new GoTime(this.unixNanoseconds + nanoseconds, this.offsetSeconds); }
  /** Return the same instant rendered in UTC, as Go's Time.UTC does. */
  inUTC(): GoTime { return new GoTime(this.unixNanoseconds); }
  /** Report whether this instant precedes other. */
  isBefore(other: GoTime): boolean { return this.unixNanoseconds < other.unixNanoseconds; }
  /** Report whether this instant follows other. */
  isAfter(other: GoTime): boolean { return this.unixNanoseconds > other.unixNanoseconds; }

  /**
   * Render Go's time.RFC3339Nano: trailing fractional zeros dropped, Z for a zero offset. Throws RangeError for
   * a year outside 0 to 9999, which Go's JSON encoding refuses as well.
   */
  formatRFC3339Nano(): string {
    const local = this.unixNanoseconds + BigInt(this.offsetSeconds) * nanosecondsPerSecond;
    const seconds = floorDivide(local, nanosecondsPerSecond);
    const fraction = local - seconds * nanosecondsPerSecond;
    const days = Number(floorDivide(seconds, BigInt(secondsPerDay)));
    const secondOfDay = Number(seconds - BigInt(days) * BigInt(secondsPerDay));
    const [year, month, day] = civilFromDays(days);
    if (year < 0 || year > 9999) throw new RangeError('Go time year is outside the range [0,9999]');
    let text = `${pad(year, 4)}-${pad(month, 2)}-${pad(day, 2)}T${pad(Math.floor(secondOfDay / 3600), 2)}:${pad(Math.floor(secondOfDay / 60) % 60, 2)}:${pad(secondOfDay % 60, 2)}`;
    if (fraction !== 0n) text += `.${fraction.toString().padStart(9, '0').replace(/0+$/, '')}`;
    if (this.offsetSeconds === 0) return `${text}Z`;
    const offsetMinutes = Math.floor(Math.abs(this.offsetSeconds) / 60);
    return `${text}${this.offsetSeconds < 0 ? '-' : '+'}${pad(Math.floor(offsetMinutes / 60), 2)}:${pad(offsetMinutes % 60, 2)}`;
  }

  /** Return the instant as a Date, truncated to milliseconds. */
  toDate(): Date { return new Date(Number(floorDivide(this.unixNanoseconds, 1_000_000n))); }
}

/** The Unix epoch in UTC. */
export const unixEpoch = new GoTime(0n);

/**
 * Parse value as Go's time.Parse(time.RFC3339Nano, value) does: fractions of any length after a period or
 * comma are truncated to nanoseconds, and an offset keeps its zone for formatting. Throws RangeError for any
 * value Go refuses.
 */
export function parseGoTime(value: string): GoTime {
  const match = rfc3339.exec(value);
  if (!match) throw new RangeError('time is not RFC 3339');
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  const fraction = match[7] ?? '';
  const offset = match[8]!;
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month)) throw new RangeError('time has an invalid date');
  if (hour > 23 || minute > 59 || second > 59) throw new RangeError('time has an invalid clock time');
  let offsetSeconds = 0;
  if (offset !== 'Z') {
    const offsetHours = Number(offset.slice(1, 3)); const offsetMinutes = Number(offset.slice(4, 6));
    if (offsetHours > 24 || offsetMinutes > 60) throw new RangeError('time has an invalid zone offset');
    offsetSeconds = (offsetHours * 3600 + offsetMinutes * 60) * (offset.startsWith('-') ? -1 : 1);
  }
  const nanoseconds = BigInt(`${fraction}000000000`.slice(0, 9));
  const localSeconds = BigInt(daysFromCivil(year, month, day)) * BigInt(secondsPerDay) + BigInt(hour * 3600 + minute * 60 + second);
  return new GoTime((localSeconds - BigInt(offsetSeconds)) * nanosecondsPerSecond + nanoseconds, offsetSeconds);
}

function floorDivide(dividend: bigint, divisor: bigint): bigint {
  const quotient = dividend / divisor;
  return dividend % divisor !== 0n && (dividend < 0n) !== (divisor < 0n) ? quotient - 1n : quotient;
}

function pad(value: number, width: number): string { return String(value).padStart(width, '0'); }

function isLeapYear(year: number): boolean { return year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0); }

function daysInMonth(year: number, month: number): number {
  if (month === 2) return isLeapYear(year) ? 29 : 28;
  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}

// Howard Hinnant's proleptic Gregorian day count, valid for year 0 as Go's calendar is.
function daysFromCivil(year: number, month: number, day: number): number {
  const shiftedYear = month <= 2 ? year - 1 : year;
  const era = Math.floor(shiftedYear / 400);
  const yearOfEra = shiftedYear - era * 400;
  const dayOfYear = Math.floor((153 * (month + (month > 2 ? -3 : 9)) + 2) / 5) + day - 1;
  const dayOfEra = yearOfEra * 365 + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100) + dayOfYear;
  return era * 146097 + dayOfEra - 719468;
}

function civilFromDays(days: number): [number, number, number] {
  const shifted = days + 719468;
  const era = Math.floor(shifted / 146097);
  const dayOfEra = shifted - era * 146097;
  const yearOfEra = Math.floor((dayOfEra - Math.floor(dayOfEra / 1460) + Math.floor(dayOfEra / 36524) - Math.floor(dayOfEra / 146096)) / 365);
  const dayOfYear = dayOfEra - (365 * yearOfEra + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100));
  const monthIndex = Math.floor((5 * dayOfYear + 2) / 153);
  const day = dayOfYear - Math.floor((153 * monthIndex + 2) / 5) + 1;
  const month = monthIndex + (monthIndex < 10 ? 3 : -9);
  return [yearOfEra + era * 400 + (month <= 2 ? 1 : 0), month, day];
}
