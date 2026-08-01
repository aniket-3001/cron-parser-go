import { call, trackHandle } from './runtime';
import {
  CronChars,
  CronDayOfMonth,
  CronDayOfWeek,
  CronField,
  CronHour,
  CronMinute,
  CronMonth,
  CronSecond,
  DayOfMonthRange,
  DayOfWeekRange,
  HourRange,
  MonthRange,
  SerializedCronField,
  SixtyRange,
} from './fields';

/** One run of values recovered from an expanded field. */
export type FieldRange = {
  start: number | CronChars;
  count: number;
  end?: number;
  step?: number;
};

export type CronFields = {
  second: CronSecond;
  minute: CronMinute;
  hour: CronHour;
  dayOfMonth: CronDayOfMonth;
  month: CronMonth;
  dayOfWeek: CronDayOfWeek;
};

export type CronFieldOverride = {
  second?: CronSecond | SixtyRange[];
  minute?: CronMinute | SixtyRange[];
  hour?: CronHour | HourRange[];
  dayOfMonth?: CronDayOfMonth | DayOfMonthRange[];
  month?: CronMonth | MonthRange[];
  dayOfWeek?: CronDayOfWeek | DayOfWeekRange[];
};

export type SerializedCronFields = {
  second: SerializedCronField;
  minute: SerializedCronField;
  hour: SerializedCronField;
  dayOfMonth: SerializedCronField;
  month: SerializedCronField;
  dayOfWeek: SerializedCronField;
};

/** The six fields of a cron expression, grouped and cross-validated. */
export class CronFieldCollection {
  readonly _handle!: number;

  // Declared with TypeScript's `private` rather than a `#` field: private
  // names are installed only by a constructor call, and _adopt builds its
  // instance with Object.create so that the port's existing field objects are
  // reused rather than rebuilt.
  private _second!: CronSecond;
  private _minute!: CronMinute;
  private _hour!: CronHour;
  private _dayOfMonth!: CronDayOfMonth;
  private _month!: CronMonth;
  private _dayOfWeek!: CronDayOfWeek;

  constructor({ second, minute, hour, dayOfMonth, month, dayOfWeek }: CronFields) {
    // Missing fields are reported one at a time, in declaration order, so the
    // message names the first thing the caller forgot.
    const required: [CronField | undefined, string][] = [
      [second, 'second'],
      [minute, 'minute'],
      [hour, 'hour'],
      [dayOfMonth, 'dayOfMonth'],
      [month, 'month'],
      [dayOfWeek, 'dayOfWeek'],
    ];
    for (const [field, name] of required) {
      if (!field) {
        throw new Error(`Validation error, Field ${name} is missing`);
      }
    }

    const handle = call<number>('fields.new', [
      second._handle,
      minute._handle,
      hour._handle,
      dayOfMonth._handle,
      month._handle,
      dayOfWeek._handle,
    ]);
    Object.defineProperty(this, '_handle', { value: handle, enumerable: false });
    trackHandle(this, handle);

    this._second = second;
    this._minute = minute;
    this._hour = hour;
    this._dayOfMonth = dayOfMonth;
    this._month = month;
    this._dayOfWeek = dayOfWeek;
  }

  /**
   * Wraps a collection the port already holds.
   *
   * Parsing builds the collection inside the port, so adopting it keeps the
   * flags the parser recorded rather than reconstructing them from values.
   */
  static _adopt(handle: number): CronFieldCollection {
    const c = Object.create(CronFieldCollection.prototype) as CronFieldCollection;
    Object.defineProperty(c, '_handle', { value: handle, enumerable: false });
    trackHandle(c, handle);

    const field = (name: string) => call<number>('fields.field', handle, name);
    c._second = CronSecond._adopt(field('second'));
    c._minute = CronMinute._adopt(field('minute'));
    c._hour = CronHour._adopt(field('hour'));
    c._dayOfMonth = CronDayOfMonth._adopt(field('dayOfMonth'));
    c._month = CronMonth._adopt(field('month'));
    c._dayOfWeek = CronDayOfWeek._adopt(field('dayOfWeek'));
    return c;
  }

  /** Builds a collection from an existing one, replacing some of its fields. */
  static from(base: CronFieldCollection, fields: CronFieldOverride): CronFieldCollection {
    return new CronFieldCollection({
      second: CronFieldCollection.resolve(CronSecond, base.second, fields.second),
      minute: CronFieldCollection.resolve(CronMinute, base.minute, fields.minute),
      hour: CronFieldCollection.resolve(CronHour, base.hour, fields.hour),
      dayOfMonth: CronFieldCollection.resolve(CronDayOfMonth, base.dayOfMonth, fields.dayOfMonth),
      month: CronFieldCollection.resolve(CronMonth, base.month, fields.month),
      dayOfWeek: CronFieldCollection.resolve(CronDayOfWeek, base.dayOfWeek, fields.dayOfWeek),
    });
  }

  private static resolve<T extends CronField, V extends unknown[]>(
    ctor: new (values: V) => T,
    baseField: T,
    override: T | V | undefined,
  ): T {
    if (!override) {
      return baseField;
    }
    if (override instanceof CronField) {
      return override as T;
    }
    return new ctor(override as V);
  }

  get second(): CronSecond {
    return this._second;
  }

  get minute(): CronMinute {
    return this._minute;
  }

  get hour(): CronHour {
    return this._hour;
  }

  get dayOfMonth(): CronDayOfMonth {
    return this._dayOfMonth;
  }

  get month(): CronMonth {
    return this._month;
  }

  get dayOfWeek(): CronDayOfWeek {
    return this._dayOfWeek;
  }

  /**
   * Folds a sorted value list into runs.
   *
   * An absent end or step is reported as undefined rather than zero, because
   * several checks downstream treat the two alike and the distinction decides
   * whether a run can be rendered at all.
   */
  static compactField(input: (number | CronChars)[]): FieldRange[] {
    return call<FieldRange[]>('compactField', input);
  }

  /** Renders one field back to expression text. */
  stringifyField(field: CronField): string {
    const name = ([
      [this._second, 'second'],
      [this._minute, 'minute'],
      [this._hour, 'hour'],
      [this._dayOfMonth, 'dayOfMonth'],
      [this._month, 'month'],
      [this._dayOfWeek, 'dayOfWeek'],
    ] as [CronField, string][]).find(([f]) => f === field)?.[1];

    if (!name) {
      throw new Error('stringifyField: field does not belong to this collection');
    }
    return call<string>('fields.formatField', this._handle, name);
  }

  /** Renders the fields back to expression text. */
  stringify(includeSeconds = false): string {
    return call<string>('fields.format', this._handle, includeSeconds);
  }

  serialize(): SerializedCronFields {
    return call<SerializedCronFields>('fields.serialize', this._handle);
  }
}
