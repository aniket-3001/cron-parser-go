import { CronField, CronFieldOptions } from './CronField';
import { CronChars, CronMax, CronMin, SixtyRange } from './types';

const MIN_VALUE = 0;
const MAX_VALUE = 59;
const FIELD_CHARS: readonly CronChars[] = Object.freeze([]);

/**
 * The "second" field of a cron expression.
 */
export class CronSecond extends CronField {
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
    super('second', values as (number | string)[], options);
  }

  get values(): SixtyRange[] {
    return super.values as SixtyRange[];
  }
}
