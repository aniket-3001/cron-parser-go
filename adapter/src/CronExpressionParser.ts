import { CronExpression, CronExpressionOptions, toBridgeOptions } from './CronExpression';
import { call } from './runtime';

/** The @-prefixed shorthands, stored in six-field form. */
export enum PredefinedExpressions {
  '@yearly' = '0 0 0 1 1 *',
  '@annually' = '0 0 0 1 1 *',
  '@monthly' = '0 0 0 1 * *',
  '@weekly' = '0 0 0 * * 0',
  '@daily' = '0 0 0 * * *',
  '@hourly' = '0 0 * * * *',
  '@minutely' = '0 * * * * *',
  '@secondly' = '* * * * * *',
  '@weekdays' = '0 0 0 * * 1-5',
  '@weekends' = '0 0 0 * * 0,6',
}

/** The six fields of a cron expression. */
export enum CronUnit {
  Second = 'Second',
  Minute = 'Minute',
  Hour = 'Hour',
  DayOfMonth = 'DayOfMonth',
  Month = 'Month',
  DayOfWeek = 'DayOfWeek',
}

/** Month names, lowercase because lookup happens after lowercasing. */
export enum Months {
  jan = 1,
  feb = 2,
  mar = 3,
  apr = 4,
  may = 5,
  jun = 6,
  jul = 7,
  aug = 8,
  sep = 9,
  oct = 10,
  nov = 11,
  dec = 12,
}

/** Day-of-week names, lowercase for the same reason. */
export enum DayOfWeek {
  sun = 0,
  mon = 1,
  tue = 2,
  wed = 3,
  thu = 4,
  fri = 5,
  sat = 6,
}

export type RawCronFields = {
  second: string;
  minute: string;
  hour: string;
  dayOfMonth: string;
  month: string;
  dayOfWeek: string;
};

/** Parses cron expressions. */
export class CronExpressionParser {
  /**
   * Parses an expression.
   *
   * Five or six fields are accepted; with five, a zero seconds field is
   * prepended. All of the work happens in the Go port.
   */
  static parse(expression: string, options: CronExpressionOptions = {}): CronExpression {
    const handle = call<number>('parse', expression, toBridgeOptions(options));
    // The original records the expanded text, so an alias reports itself in
    // six-field form rather than as the alias.
    const expanded =
      PredefinedExpressions[expression as keyof typeof PredefinedExpressions] || expression;
    return CronExpression._adopt(handle, { ...options, expression: expanded });
  }
}

export default CronExpressionParser;
