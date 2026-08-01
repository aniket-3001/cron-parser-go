/**
 * Proves the bridge is wired correctly before the original suite is pointed at
 * it. This file is ours, not part of the pinned original suite.
 */
import CronExpressionParser, { CronDate, CronExpression, CronSecond, CronFieldCollection } from '../../adapter/src';
import { seededRandom } from '../../adapter/src/utils/random';

describe('adapter smoke test', () => {
  test('parses and iterates', () => {
    const e = CronExpressionParser.parse('0 0 * * *', {
      currentDate: new Date('2026-01-01T00:00:00Z'),
      tz: 'UTC',
    });
    expect(e.next().toISOString()).toBe('2026-01-02T00:00:00.000Z');
    expect(e.next().toISOString()).toBe('2026-01-03T00:00:00.000Z');
  });

  test('reproduces error messages exactly', () => {
    expect(() => CronExpressionParser.parse('61 * * * *')).toThrow(
      'Constraint error, got value 61 expected range 0-59',
    );
  });

  test('exposes parsed fields', () => {
    const e = CronExpressionParser.parse('*/20 9-11 * * 1-5');
    expect(e.fields.minute.values).toEqual([0, 20, 40]);
    expect(e.fields.hour.values).toEqual([9, 10, 11]);
    expect(e.fields.dayOfMonth.isWildcard).toBe(true);
  });

  test('renders back to text', () => {
    expect(CronExpressionParser.parse('0 0 * * MON#2').stringify()).toBe('0 0 * * 1#2');
    expect(CronExpressionParser.parse('@daily').toString()).toBe('0 0 0 * * *');
  });

  test('CronDate is a real class with working arithmetic', () => {
    const d = new CronDate(new Date('2024-02-29T12:00:00Z'), 'UTC');
    expect(d).toBeInstanceOf(CronDate);
    d.addYear();
    // luxon clamps rather than overflowing into March.
    expect(d.toISOString()).toBe('2025-02-28T12:00:00.000Z');
  });

  test('CronDate parses strings', () => {
    expect(new CronDate('2021-01-04T10:00:00', 'UTC').toISOString()).toBe('2021-01-04T10:00:00.000Z');
    expect(new CronDate('2021-01-04 10:00:00', 'UTC').toISOString()).toBe('2021-01-04T10:00:00.000Z');
  });

  test('prototype methods are spyable', () => {
    const spy = jest.spyOn(CronDate.prototype, 'applyDateOperation');
    const e = CronExpressionParser.parse('0 10 * * * *', {
      currentDate: new Date('2023-01-01T00:59:30.000Z'),
      tz: 'UTC',
    });
    expect(e.next().toISOString()).toBe('2023-01-01T01:10:00.000Z');
    spy.mockRestore();
  });

  test('fields can be built by hand', () => {
    const s = new CronSecond([0]);
    expect(s.values).toEqual([0]);
    expect(() => new CronSecond([60] as never)).toThrow(
      'CronSecond Validation error, got value 60 expected range 0-59',
    );
  });

  test('compactField folds runs', () => {
    expect(CronFieldCollection.compactField([0, 15, 30, 45])).toEqual([
      { start: 0, count: 4, end: 45, step: 15 },
    ]);
  });

  test('seededRandom is deterministic and matches the original', () => {
    const r = seededRandom('hello');
    expect(r()).toBeCloseTo(0.63119658012874424, 15);
    expect(r()).toBeCloseTo(0.79834905150346458, 15);
  });

  test('iterator protocol works', () => {
    const e = CronExpressionParser.parse('0 0 * * *', {
      currentDate: new Date('2026-01-01T00:00:00Z'),
      tz: 'UTC',
    });
    const seen: string[] = [];
    for (const d of e) {
      seen.push(d.toISOString()!.slice(0, 10));
      if (seen.length === 3) break;
    }
    expect(seen).toEqual(['2026-01-02', '2026-01-03', '2026-01-04']);
  });

  test('fieldsToExpression builds without text', () => {
    const e = CronExpression.fieldsToExpression(
      CronExpressionParser.parse('0 0 12 * * *').fields,
      { currentDate: new Date('2026-01-01T00:00:00Z'), tz: 'UTC' },
    );
    expect(e.next().toISOString()).toBe('2026-01-01T12:00:00.000Z');
  });
});
