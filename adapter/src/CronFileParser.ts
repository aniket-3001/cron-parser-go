import { CronExpression } from './CronExpression';
import { CronExpressionParser } from './CronExpressionParser';

export type CronFileParserResult = {
  variables: { [key: string]: string };
  expressions: CronExpression[];
  errors: { [key: string]: unknown };
};

/**
 * Reads crontab files.
 *
 * File reading stays in JavaScript rather than moving into the port. The
 * original's tests replace the filesystem module and assert on the calls made
 * to it, so the read has to happen through the same module they mock. Parsing
 * the content is the port's work.
 */
export class CronFileParser {
  /** Parses a crontab file. */
  static async parseFile(filePath: string): Promise<CronFileParserResult> {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { readFile } = require('fs/promises');
    const data = await readFile(filePath, 'utf8');
    return CronFileParser.#parseContent(data);
  }

  /** Parses a crontab file synchronously. */
  static parseFileSync(filePath: string): CronFileParserResult {
    // eslint-disable-next-line @typescript-eslint/no-require-imports
    const { readFileSync } = require('fs');
    const data = readFileSync(filePath, 'utf8');
    return CronFileParser.#parseContent(data);
  }

  static #parseContent(data: string): CronFileParserResult {
    const result: CronFileParserResult = { variables: {}, expressions: [], errors: {} };

    for (const block of data.split('\n')) {
      const entry = block.trim();
      if (entry.length === 0 || entry.startsWith('#')) {
        continue;
      }

      const variableMatch = entry.match(/^(.*)=(.*)$/);
      if (variableMatch) {
        const [, key, value] = variableMatch;
        result.variables[key] = value.replace(/["']/g, '');
        continue;
      }

      try {
        // Only the first five atoms are the schedule; the rest is the command.
        const atoms = entry.split(' ');
        result.expressions.push(CronExpressionParser.parse(atoms.slice(0, 5).join(' ')));
      } catch (err: unknown) {
        result.errors[entry] = err;
      }
    }

    return result;
  }
}

export default CronFileParser;
