import { call } from '../runtime';
import { CronField, CronFieldOptions } from './CronField';
import { CronChars, CronMax, CronMin, DayOfWeekRange } from './types';

const MIN_VALUE = 0;
const MAX_VALUE = 7;
const FIELD_CHARS: readonly CronChars[] = Object.freeze(['L']);

/**
 * The "day of week" field of a cron expression.
 */
export class CronDayOfWeek extends CronField {
  static get min(): CronMin {
    return MIN_VALUE;
  }

  static get max(): CronMax {
    return MAX_VALUE;
  }

  static get chars(): readonly CronChars[] {
    return FIELD_CHARS;
  }

  static get validChars(): RegExp {
    return /^[?,*\dLH#/-]+$|^.*H\(\d+-\d+\)\/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H\/\d+.*$/;
  }

  constructor(values: DayOfWeekRange[], options?: CronFieldOptions) {
    super('dayOfWeek', values as (number | string)[], options);
  }

  get values(): DayOfWeekRange[] {
    return super.values as DayOfWeekRange[];
  }

  /** The N of a `#N` suffix, or 0 when there is none. */
  get nthDay(): number {
    return call<number>('field.get', this._handle, 'nthDay');
  }
}
