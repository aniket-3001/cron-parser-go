import { call, trackHandle } from './runtime';

/** Units a date operation can act on. */
export enum TimeUnit {
  Second = 'Second',
  Minute = 'Minute',
  Hour = 'Hour',
  Day = 'Day',
  Month = 'Month',
  Year = 'Year',
}

/** Direction of a date operation. */
export enum DateMathOp {
  Add = 'Add',
  Subtract = 'Subtract',
}

/**
 * Days per month, with February listed as 29.
 *
 * The value is deliberately leap-permissive: cross-field validation consults it
 * to decide whether an explicit day of month could ever occur, and must accept
 * 29 February.
 */
export const DAYS_IN_MONTH: readonly number[] = Object.freeze([
  31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31,
]);

/**
 * A wall-clock instant in a timezone.
 *
 * Every method forwards to the Go port. The class exists so that the original's
 * tests, which construct CronDate directly and spy on its prototype, keep
 * working against a port that holds the state elsewhere.
 */
export class CronDate {
  /** Handle to the Go-side object. */
  readonly _handle: number;

  constructor(timestamp?: CronDate | Date | number | string, tz?: string) {
    const zone = tz ?? '';

    if (timestamp === undefined || timestamp === null) {
      this._handle = call<number>('date.now', zone);
    } else if (timestamp instanceof CronDate) {
      // Copying keeps the daylight-saving bookkeeping, which the search loop
      // carries between iterations.
      const copied = call<number>('date.clone', timestamp._handle);
      this._handle = tz ? call<number>('date.withLocation', copied, zone) : copied;
    } else if (timestamp instanceof Date) {
      // An invalid Date has a NaN time. The port cannot represent that, and the
      // original rejects it here rather than carrying it forward.
      if (Number.isNaN(timestamp.getTime())) {
        throw new Error(`CronDate: unhandled timestamp: ${timestamp}`);
      }
      this._handle = call<number>('date.fromMillis', timestamp.getTime(), zone);
    } else if (typeof timestamp === 'number') {
      this._handle = call<number>('date.fromMillis', timestamp, zone);
    } else {
      this._handle = call<number>('date.fromString', timestamp, zone);
    }

    trackHandle(this, this._handle);
  }

  /** Reads a scalar property from the Go object. */
  private get<T>(name: string): T {
    return call<T>('date.get', this._handle, name);
  }

  private put(name: string, value: number): void {
    call('date.set', this._handle, name, value);
  }

  // --- daylight saving bookkeeping ---------------------------------------

  get dstStart(): number | null {
    return this.get<number | null>('dstStart');
  }

  set dstStart(value: number | null) {
    this.put('dstStart', value ?? -1);
  }

  get dstEnd(): number | null {
    return this.get<number | null>('dstEnd');
  }

  set dstEnd(value: number | null) {
    this.put('dstEnd', value ?? -1);
  }

  // --- arithmetic ---------------------------------------------------------

  addYear(): void {
    call('date.arith', this._handle, 'addYear');
  }

  addMonth(): void {
    call('date.arith', this._handle, 'addMonth');
  }

  addDay(): void {
    call('date.arith', this._handle, 'addDay');
  }

  addHour(): void {
    call('date.arith', this._handle, 'addHour');
  }

  addMinute(): void {
    call('date.arith', this._handle, 'addMinute');
  }

  addSecond(): void {
    call('date.arith', this._handle, 'addSecond');
  }

  subtractYear(): void {
    call('date.arith', this._handle, 'subtractYear');
  }

  subtractMonth(): void {
    call('date.arith', this._handle, 'subtractMonth');
  }

  subtractDay(): void {
    call('date.arith', this._handle, 'subtractDay');
  }

  subtractHour(): void {
    call('date.arith', this._handle, 'subtractHour');
  }

  subtractMinute(): void {
    call('date.arith', this._handle, 'subtractMinute');
  }

  subtractSecond(): void {
    call('date.arith', this._handle, 'subtractSecond');
  }

  addUnit(unit: TimeUnit): void {
    call('date.addUnit', this._handle, unit);
  }

  subtractUnit(unit: TimeUnit): void {
    call('date.subtractUnit', this._handle, unit);
  }

  invokeDateOperation(verb: DateMathOp, unit: TimeUnit): void {
    call('date.invoke', this._handle, verb, unit);
  }

  /**
   * Performs a date operation and records any daylight-saving transition it
   * crossed.
   *
   * Kept as a prototype method because the original's tests spy on it to check
   * that iteration jumps to the next matching value rather than stepping.
   */
  applyDateOperation(op: DateMathOp, unit: TimeUnit, hoursLength?: number): void {
    call('date.apply', this._handle, op, unit, hoursLength ?? 0);
  }

  // --- local component accessors -----------------------------------------

  getDate(): number {
    return this.get<number>('day');
  }

  getFullYear(): number {
    return this.get<number>('year');
  }

  getDay(): number {
    return this.get<number>('weekday');
  }

  /** Zero-based, matching Date.prototype.getMonth. */
  getMonth(): number {
    return this.get<number>('month');
  }

  getHours(): number {
    return this.get<number>('hour');
  }

  getMinutes(): number {
    return this.get<number>('minute');
  }

  getSeconds(): number {
    return this.get<number>('second');
  }

  getMilliseconds(): number {
    return this.get<number>('millisecond');
  }

  /**
   * The timezone this date is observed in.
   *
   * An expression built with this date as its starting point, and no timezone
   * of its own, inherits this one.
   */
  get zoneName(): string {
    return this.get<string>('zone');
  }

  /** Offset from UTC in minutes, positive east of Greenwich. */
  getUTCOffset(): number {
    return this.get<number>('offsetMinutes');
  }

  getTime(): number {
    return this.get<number>('time');
  }

  // --- UTC component accessors -------------------------------------------

  getUTCDate(): number {
    return this.get<number>('utcDay');
  }

  getUTCFullYear(): number {
    return this.get<number>('utcYear');
  }

  getUTCDay(): number {
    return this.get<number>('utcWeekday');
  }

  getUTCMonth(): number {
    return this.get<number>('utcMonth');
  }

  getUTCHours(): number {
    return this.get<number>('utcHour');
  }

  getUTCMinutes(): number {
    return this.get<number>('utcMinute');
  }

  getUTCSeconds(): number {
    return this.get<number>('utcSecond');
  }

  // --- setters ------------------------------------------------------------

  setDate(d: number): void {
    this.put('day', d);
  }

  setFullYear(y: number): void {
    this.put('year', y);
  }

  setDay(d: number): void {
    this.put('weekday', d);
  }

  setMonth(m: number): void {
    this.put('month', m);
  }

  setHours(h: number): void {
    this.put('hour', h);
  }

  setMinutes(m: number): void {
    this.put('minute', m);
  }

  setSeconds(s: number): void {
    this.put('second', s);
  }

  setMilliseconds(s: number): void {
    this.put('millisecond', s);
  }

  setStartOfDay(): void {
    call('date.startOfDay', this._handle);
  }

  setEndOfDay(): void {
    call('date.endOfDay', this._handle);
  }

  // --- predicates and rendering ------------------------------------------

  isLastDayOfMonth(): boolean {
    return this.get<boolean>('isLastDayOfMonth');
  }

  isLastWeekdayOfMonth(): boolean {
    return this.get<boolean>('isLastWeekdayOfMonth');
  }

  toISOString(): string | null {
    return this.get<string>('iso');
  }

  toJSON(): string | null {
    return this.get<string>('local');
  }

  toDate(): Date {
    return new Date(this.getTime());
  }

  toString(): string {
    return this.toDate().toString();
  }
}

export default CronDate;
