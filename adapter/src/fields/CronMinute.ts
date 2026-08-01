import { CronField, CronFieldOptions } from './CronField';
import { CronChars, CronMax, CronMin, SixtyRange } from './types';

const MIN_VALUE = 0;
const MAX_VALUE = 59;
const FIELD_CHARS: readonly CronChars[] = Object.freeze([]);

/**
 * The "minute" field of a cron expression.
 */
export class CronMinute extends CronField {
  static get min(): CronMin {
    return MIN_VALUE;
  }

  static get max(): CronMax {
    return MAX_VALUE;
  }

  static get chars(): readonly CronChars[] {
    return FIELD_CHARS;
  }

  constructor(values: SixtyRange[], options?: CronFieldOptions) {
    super('minute', values as (number | string)[], options);
  }

  get values(): SixtyRange[] {
    return super.values as SixtyRange[];
  }
}
