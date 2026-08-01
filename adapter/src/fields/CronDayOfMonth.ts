import { CronField, CronFieldOptions } from './CronField';
import { CronChars, CronMax, CronMin, DayOfMonthRange } from './types';

const MIN_VALUE = 1;
const MAX_VALUE = 31;
const FIELD_CHARS: readonly CronChars[] = Object.freeze(['L']);

/**
 * The "day of month" field of a cron expression.
 */
export class CronDayOfMonth extends CronField {
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
    return /^[?,*\dLH/-]+$|^.*H\(\d+-\d+\)\/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H\/\d+.*$/;
  }

  constructor(values: DayOfMonthRange[], options?: CronFieldOptions) {
    super('dayOfMonth', values as (number | string)[], options);
  }

  get values(): DayOfMonthRange[] {
    return super.values as DayOfMonthRange[];
  }
}
