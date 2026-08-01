import { CronField, CronFieldOptions } from './CronField';
import { CronChars, CronMax, CronMin, HourRange } from './types';

const MIN_VALUE = 0;
const MAX_VALUE = 23;
const FIELD_CHARS: readonly CronChars[] = Object.freeze([]);

/**
 * The "hour" field of a cron expression.
 */
export class CronHour extends CronField {
  static get min(): CronMin {
    return MIN_VALUE;
  }

  static get max(): CronMax {
    return MAX_VALUE;
  }

  static get chars(): readonly CronChars[] {
    return FIELD_CHARS;
  }

  constructor(values: HourRange[], options?: CronFieldOptions) {
    super('hour', values as (number | string)[], options);
  }

  get values(): HourRange[] {
    return super.values as HourRange[];
  }
}
