import { CronDate } from './CronDate';
import { CronFieldCollection } from './CronFieldCollection';
import { call, trackHandle } from './runtime';

export type CronExpressionOptions = {
  currentDate?: Date | string | number | CronDate;
  endDate?: Date | string | number | CronDate;
  startDate?: Date | string | number | CronDate;
  tz?: string;
  expression?: string;
  hashSeed?: string;
  strict?: boolean;
};

/** Reported when iteration leaves the configured window. */
export const TIME_SPAN_OUT_OF_BOUNDS_ERROR_MESSAGE = 'Out of the time span range';

/** Reported when a search exceeds its iteration limit, meaning it can never match. */
export const LOOPS_LIMIT_EXCEEDED_ERROR_MESSAGE = 'Invalid expression, loop limit exceeded';

/** Converts the several accepted timestamp shapes to epoch milliseconds. */
function toMillis(value: Date | string | number | CronDate | undefined, tz?: string): number | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value === 'number') {
    return value;
  }
  if (value instanceof Date) {
    return value.getTime();
  }
  if (value instanceof CronDate) {
    return value.getTime();
  }
  // A string is handed to the port, which parses the formats the original does.
  return new CronDate(value, tz).getTime();
}

/** Builds the option bag the bridge understands. */
export function toBridgeOptions(options: CronExpressionOptions = {}): Record<string, unknown> {
  const out: Record<string, unknown> = {};

  // A CronDate carries its own zone, and the original's copy constructor keeps
  // it, so an expression given one as its starting point and no timezone of its
  // own is evaluated in that date's zone.
  const inherited = options.currentDate instanceof CronDate ? options.currentDate.zoneName : undefined;
  const tz = options.tz ?? inherited;
  if (tz !== undefined) out.tz = tz;
  if (options.hashSeed !== undefined) out.hashSeed = options.hashSeed;
  if (options.strict !== undefined) out.strict = options.strict;

  const current = toMillis(options.currentDate, tz);
  if (current !== undefined) out.currentDate = current;
  const start = toMillis(options.startDate, tz);
  if (start !== undefined) out.startDate = start;
  const end = toMillis(options.endDate, tz);
  if (end !== undefined) out.endDate = end;

  return out;
}

/**
 * A parsed cron expression and a cursor into the schedule it describes.
 *
 * The schedule is computed by the Go port; this forwards and wraps the results
 * as CronDate instances, which is what the original returns.
 */
export class CronExpression {
  readonly _handle!: number;
  // Declared with TypeScript's `private` rather than `#`: private names exist
  // only on instances built by a constructor, and _adopt uses Object.create so
  // that the port's existing expression is reused.
  private _options!: CronExpressionOptions;
  private _tz?: string;
  private _fields?: CronFieldCollection;

  constructor(fields: CronFieldCollection, options: CronExpressionOptions = {}) {
    const handle = call<number>('fields.toExpression', fields._handle, toBridgeOptions(options));
    Object.defineProperty(this, '_handle', { value: handle, enumerable: false });
    this._options = options;
    this._tz = options.tz;
    this._fields = fields;
    trackHandle(this, handle);
  }

  /**
   * Wraps an expression the port already holds.
   *
   * Parsing builds the expression inside the port, so its fields are adopted
   * lazily rather than rebuilt here.
   */
  static _adopt(handle: number, options: CronExpressionOptions = {}): CronExpression {
    const e = Object.create(CronExpression.prototype) as CronExpression;
    Object.defineProperty(e, '_handle', { value: handle, enumerable: false });
    e._options = options;
    e._tz = options.tz;
    trackHandle(e, handle);
    return e;
  }

  /** Builds an expression from fields rather than from text. */
  static fieldsToExpression(fields: CronFieldCollection, options: CronExpressionOptions = {}): CronExpression {
    return new CronExpression(fields, options);
  }

  get fields(): CronFieldCollection {
    if (!this._fields) {
      this._fields = CronFieldCollection._adopt(call<number>('expr.fields', this._handle));
    }
    return this._fields;
  }

  /** Wraps an instant from the port as a CronDate in the expression's zone. */
  private _date(millis: number): CronDate {
    return new CronDate(millis, this._tz);
  }

  next(): CronDate {
    return this._date(call<number>('expr.next', this._handle));
  }

  prev(): CronDate {
    return this._date(call<number>('expr.prev', this._handle));
  }

  hasNext(): boolean {
    return call<boolean>('expr.hasNext', this._handle);
  }

  hasPrev(): boolean {
    return call<boolean>('expr.hasPrev', this._handle);
  }

  take(limit: number): CronDate[] {
    return call<number[]>('expr.take', this._handle, limit).map((ms) => this._date(ms));
  }

  reset(newDate?: Date | CronDate): void {
    const millis = toMillis(newDate ?? this._options.currentDate, this._tz);
    call('expr.reset', this._handle, millis ?? null);
  }

  stringify(includeSeconds = false): string {
    return call<string>('expr.format', this._handle, includeSeconds);
  }

  includesDate(date: Date | CronDate): boolean {
    // Routed through CronDate rather than read directly, so that an invalid
    // Date is rejected here as the original rejects it, instead of reaching the
    // port as a NaN timestamp.
    const millis = date instanceof CronDate ? date.getTime() : new CronDate(date, this._tz).getTime();
    return call<boolean>('expr.includes', this._handle, millis);
  }

  toString(): string {
    return this._options.expression || call<string>('expr.string', this._handle);
  }

  /** Iterates the schedule forwards, stopping when it is exhausted. */
  [Symbol.iterator](): Iterator<CronDate> {
    return {
      next: () => {
        try {
          return { value: this.next(), done: false };
        } catch {
          return { value: undefined as unknown as CronDate, done: true };
        }
      },
    };
  }
}

export default CronExpression;
