// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// The ticketChanged cursor shares its bytes with the Go connector, so its times keep Go's nanosecond
// precision, fixed zone offset, and RFC 3339 rendering, which a JavaScript Date cannot hold.

const nanosecondsPerSecond = 1_000_000_000n;
const secondsPerDay = 86_400n;
const rfc3339Pattern = /^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{1,2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?(Z|[+-][0-9]{2}:[0-9]{2})?$/;
const httpDatePatterns = [
  // Go's http.TimeFormat, time.RFC850, and time.ANSIC, the layouts http.ParseTime accepts.
  /^([A-Za-z]{3}), ([0-9]{2}) ([A-Za-z]{3}) ([0-9]{4}) ([0-9]{1,2}):([0-9]{2}):([0-9]{2}) GMT$/,
  /^([A-Za-z]{6,9}), ([0-9]{2})-([A-Za-z]{3})-([0-9]{2}) ([0-9]{1,2}):([0-9]{2}):([0-9]{2}) [A-Z]{3,5}$/,
  /^([A-Za-z]{3}) ([A-Za-z]{3}) ([ 0-9][0-9]) ([0-9]{1,2}):([0-9]{2}):([0-9]{2}) ([0-9]{4})$/,
] as const;
const shortDayNames = ['sun', 'mon', 'tue', 'wed', 'thu', 'fri', 'sat'];
const longDayNames = ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday'];
const shortMonthNames = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

/** An instant in Unix nanoseconds with the fixed zone offset Go keeps for formatting. */
export class GoTime {
  /** Bind the instant and offset. */
  constructor(readonly unixNanoseconds: bigint, readonly offsetSeconds = 0) {}

  /** Return the instant of a Date in UTC. */
  static fromDate(date: Date): GoTime { return new GoTime(BigInt(date.getTime()) * 1_000_000n); }

  /** Return the instant moved by nanoseconds, keeping the offset as Go's Time.Add does. */
  add(nanoseconds: bigint): GoTime { return new GoTime(this.unixNanoseconds + nanoseconds, this.offsetSeconds); }
  /** Return the same instant rendered in UTC, as Go's Time.UTC does. */
  inUTC(): GoTime { return new GoTime(this.unixNanoseconds); }
  /** Report whether this instant precedes other. */
  isBefore(other: GoTime): boolean { return this.unixNanoseconds < other.unixNanoseconds; }
  /** Report whether this instant follows other. */
  isAfter(other: GoTime): boolean { return this.unixNanoseconds > other.unixNanoseconds; }
  /** Return the instant in Unix microseconds, floored as Go's Time.UnixMicro does. */
  get unixMicroseconds(): bigint { return floorDivide(this.unixNanoseconds, 1000n); }
  /** Return the instant as a Date, truncated to milliseconds. */
  toDate(): Date { return new Date(Number(floorDivide(this.unixNanoseconds, 1_000_000n))); }

  /** Render Go's time.RFC3339Nano: trailing fractional zeros dropped, Z for a zero offset; throws outside years 0 to 9999. */
  formatRFC3339Nano(): string {
    const local = this.unixNanoseconds + BigInt(this.offsetSeconds) * nanosecondsPerSecond;
    const seconds = floorDivide(local, nanosecondsPerSecond);
    const fraction = local - seconds * nanosecondsPerSecond;
    let text = formatCivilSeconds(seconds);
    if (fraction) text += `.${fraction.toString().padStart(9, '0').replace(/0+$/, '')}`;
    if (this.offsetSeconds === 0) return `${text}Z`;
    const offsetMinutes = Math.floor(Math.abs(this.offsetSeconds) / 60);
    return `${text}${this.offsetSeconds < 0 ? '-' : '+'}${pad(Math.floor(offsetMinutes / 60))}:${pad(offsetMinutes % 60)}`;
  }

  /** Render UTC with exactly six fractional digits, Go's "2006-01-02T15:04:05.000000Z07:00" layout. */
  formatQueryMicroseconds(): string {
    const seconds = floorDivide(this.unixNanoseconds, nanosecondsPerSecond);
    const microseconds = (this.unixNanoseconds - seconds * nanosecondsPerSecond) / 1000n;
    return `${formatCivilSeconds(seconds)}.${microseconds.toString().padStart(6, '0')}Z`;
  }
}

/** Parse value as Go's time.Parse(time.RFC3339Nano, value) does; throws for any value Go refuses. */
export function parseGoTime(value: string): GoTime {
  const match = rfc3339Pattern.exec(value);
  if (!match || match[8] === undefined) throw new RangeError('time is not RFC 3339');
  return parseRFC3339Match(match);
}

/**
 * Parse a Gorgias ISO 8601 timestamp in UTC as the Go connector's parseGorgiasTimestamp does: RFC 3339 with an
 * offset, or Gorgias's documented offset-less UTC form, with or without a fraction. Throws for any value Go refuses.
 */
export function parseGorgiasTime(value: string): GoTime {
  const match = rfc3339Pattern.exec(value);
  if (!match) throw new RangeError('timestamp is not ISO 8601');
  return parseRFC3339Match(match).inUTC();
}

/** Parse an HTTP Date header as Go's http.ParseTime does, in UTC; undefined when no layout matches. */
export function parseHTTPDate(value: string): GoTime | undefined {
  for (const [index, pattern] of httpDatePatterns.entries()) {
    const match = pattern.exec(value);
    if (!match) continue;
    let [weekday, day, month, year, hour, minute, second] = match.slice(1) as string[] as [string, string, string, string, string, string, string];
    let isWeekdayKnown = shortDayNames.includes(weekday.toLowerCase());
    if (index === 1) {
      isWeekdayKnown = longDayNames.includes(weekday.toLowerCase());
      year = String((Number(year) >= 69 ? 1900 : 2000) + Number(year));
    } else if (index === 2) {
      [weekday, month, day, hour, minute, second, year] = match.slice(1) as string[] as [string, string, string, string, string, string, string];
      isWeekdayKnown = shortDayNames.includes(weekday.toLowerCase());
    }
    const monthNumber = shortMonthNames.indexOf(month.toLowerCase()) + 1;
    if (!isWeekdayKnown || monthNumber === 0) continue;
    try { return civilTime(Number(year), monthNumber, Number(day.trim()), Number(hour), Number(minute), Number(second), 0n, 0); }
    catch { continue; }
  }
  return undefined;
}

function parseRFC3339Match(match: RegExpExecArray): GoTime {
  const [year, month, day, hour, minute, second] = match.slice(1, 7).map(Number) as [number, number, number, number, number, number];
  const fraction = match[7] ?? '';
  const offset = match[8] ?? 'Z';
  let offsetSeconds = 0;
  if (offset !== 'Z') {
    const offsetHours = Number(offset.slice(1, 3));
    const offsetMinutes = Number(offset.slice(4, 6));
    if (offsetHours > 24 || offsetMinutes > 60) throw new RangeError('time has an invalid zone offset');
    offsetSeconds = (offsetHours * 3600 + offsetMinutes * 60) * (offset.startsWith('-') ? -1 : 1);
  }
  return civilTime(year, month, day, hour, minute, second, BigInt(`${fraction}000000000`.slice(0, 9)), offsetSeconds);
}

function civilTime(year: number, month: number, day: number, hour: number, minute: number, second: number, nanoseconds: bigint, offsetSeconds: number): GoTime {
  if (month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month)) throw new RangeError('time has an invalid date');
  if (hour > 23 || minute > 59 || second > 59) throw new RangeError('time has an invalid clock time');
  const localSeconds = BigInt(daysFromCivil(year, month, day)) * secondsPerDay + BigInt(hour * 3600 + minute * 60 + second);
  return new GoTime((localSeconds - BigInt(offsetSeconds)) * nanosecondsPerSecond + nanoseconds, offsetSeconds);
}

function formatCivilSeconds(seconds: bigint): string {
  const days = floorDivide(seconds, secondsPerDay);
  const secondOfDay = Number(seconds - days * secondsPerDay);
  const [year, month, day] = civilFromDays(Number(days));
  if (year < 0 || year > 9999) throw new RangeError('Go time year is outside the range [0,9999]');
  const hour = Math.floor(secondOfDay / 3600);
  const minute = Math.floor((secondOfDay % 3600) / 60);
  return `${String(year).padStart(4, '0')}-${pad(month)}-${pad(day)}T${pad(hour)}:${pad(minute)}:${pad(secondOfDay % 60)}`;
}

const pad = (value: number): string => String(value).padStart(2, '0');
const floorDivide = (dividend: bigint, divisor: bigint): bigint => {
  const quotient = dividend / divisor;
  return dividend % divisor !== 0n && (dividend < 0n) !== (divisor < 0n) ? quotient - 1n : quotient;
};
const isLeapYear = (year: number): boolean => year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);

function daysInMonth(year: number, month: number): number {
  if (month === 2) return isLeapYear(year) ? 29 : 28;
  return [4, 6, 9, 11].includes(month) ? 30 : 31;
}

// Howard Hinnant's days_from_civil and civil_from_days, exact for every proleptic Gregorian date.
function daysFromCivil(year: number, month: number, day: number): number {
  const shiftedYear = month <= 2 ? year - 1 : year;
  const era = Math.floor(shiftedYear / 400);
  const yearOfEra = shiftedYear - era * 400;
  const dayOfYear = Math.floor((153 * (month + (month > 2 ? -3 : 9)) + 2) / 5) + day - 1;
  const dayOfEra = yearOfEra * 365 + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100) + dayOfYear;
  return era * 146_097 + dayOfEra - 719_468;
}

function civilFromDays(days: number): [number, number, number] {
  const shifted = days + 719_468;
  const era = Math.floor(shifted / 146_097);
  const dayOfEra = shifted - era * 146_097;
  const yearOfEra = Math.floor((dayOfEra - Math.floor(dayOfEra / 1460) + Math.floor(dayOfEra / 36_524) - Math.floor(dayOfEra / 146_096)) / 365);
  const dayOfYear = dayOfEra - (365 * yearOfEra + Math.floor(yearOfEra / 4) - Math.floor(yearOfEra / 100));
  const monthPosition = Math.floor((5 * dayOfYear + 2) / 153);
  const day = dayOfYear - Math.floor((153 * monthPosition + 2) / 5) + 1;
  const month = monthPosition + (monthPosition < 10 ? 3 : -9);
  return [yearOfEra + era * 400 + (month <= 2 ? 1 : 0), month, day];
}
