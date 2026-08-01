import { CronField, CronFieldOptions } from './CronField';
import { CronChars, CronMax, CronMin, MonthRange } from './types';

const MIN_VALUE = 1;
const MAX_VALUE = 12;
const FIELD_CHARS: readonly CronChars[] = Object.freeze([]);

/**
 * The "month" field of a cron expression.
 */
export class CronMonth extends CronField {
  static get min(): CronMin {
    return MIN_VALUE;
  }

  static get max(): CronMax {
    return MAX_VALUE;
  }

  static get chars(): readonly CronChars[] {
    return FIELD_CHARS;
  }

  constructor(values: MonthRange[], options?: CronFieldOptions) {
    super('month', values as (number | string)[], options);
  }

  get values(): MonthRange[] {
    return super.values as MonthRange[];
  }
}
