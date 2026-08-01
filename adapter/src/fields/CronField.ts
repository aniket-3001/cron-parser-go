import { call, trackHandle } from '../runtime';
import { CronChars, CronConstraints, CronFieldType, CronMax, CronMin } from './types';

/** A field reduced to its wildcard flag and values. */
export type SerializedCronField = {
  wildcard: boolean;
  values: (number | string)[];
};

/** What the parser knows about a field beyond its values. */
export type CronFieldOptions = {
  rawValue?: string;
  wildcard?: boolean;
  nthDayOfWeek?: number;
};

/**
 * Base class for the six cron fields.
 *
 * The values and their validation live in the Go port; this holds a handle and
 * forwards. Subclasses supply only the unit name, because the port models the
 * difference between fields as data rather than as behaviour.
 */
export abstract class CronField {
  readonly _handle: number;

  protected constructor(unit: string, values: (number | string)[], options: CronFieldOptions = {}) {
    // The port takes a slice, so a non-array cannot reach it. The check is
    // kept here because it is the boundary where the type is still unknown.
    if (!Array.isArray(values)) {
      throw new Error(`${new.target.name} Validation error, values is not an array`);
    }
    this._handle = call<number>('field.new', unit, values, options);
    trackHandle(this, this._handle);
  }

  /**
   * Wraps a field the port already holds.
   *
   * Parsing builds its fields inside the port, and those carry flags that
   * cannot be recovered from the values alone — whether a wildcard was written
   * as `*` or as `?`, for instance. Adopting the existing field preserves them,
   * where rebuilding from values would not.
   */
  static _adopt<T extends CronField>(this: abstract new (...args: never[]) => T, handle: number): T {
    const instance = Object.create(this.prototype) as T;
    Object.defineProperty(instance, '_handle', { value: handle, enumerable: false });
    trackHandle(instance, handle);
    return instance;
  }

  /** Minimum permitted value. */
  /* istanbul ignore next */ static get min(): CronMin {
    throw new Error('min must be overridden');
  }

  /** Maximum permitted value. */
  /* istanbul ignore next */ static get max(): CronMax {
    throw new Error('max must be overridden');
  }

  /** Special characters this field permits. */
  static get chars(): readonly CronChars[] {
    return Object.freeze([]);
  }

  /** Pattern every raw field value must match. */
  static get validChars(): RegExp {
    return /^[?,*\dH/-]+$|^.*H\(\d+-\d+\)\/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H\/\d+.*$/;
  }

  /**
   * Finds the next value strictly after current, or strictly before it when
   * reverse is set, reporting null when there is none.
   */
  static findNearestValueInList(
    values: number[],
    currentValue: number,
    reverse: boolean,
  ): number | null {
    return call<number | null>('findNearestValueInList', values, currentValue, reverse);
  }

  static get constraints(): CronConstraints {
    return { min: this.min, max: this.max, chars: this.chars, validChars: this.validChars };
  }

  get min(): number {
    return call<number>('field.get', this._handle, 'min');
  }

  get max(): number {
    return call<number>('field.get', this._handle, 'max');
  }

  get chars(): readonly string[] {
    return (this.constructor as typeof CronField).chars;
  }

  get hasLastChar(): boolean {
    return call<boolean>('field.get', this._handle, 'hasLast');
  }

  get hasQuestionMarkChar(): boolean {
    return call<boolean>('field.get', this._handle, 'hasQuestion');
  }

  get isWildcard(): boolean {
    return call<boolean>('field.get', this._handle, 'isWildcard');
  }

  get values(): CronFieldType {
    return call<(number | string)[]>('field.values', this._handle) as CronFieldType;
  }

  serialize(): SerializedCronField {
    return { wildcard: this.isWildcard, values: this.values as (number | string)[] };
  }
}
