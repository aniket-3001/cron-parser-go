import { call, trackHandle } from '../runtime';

/** A pseudorandom number generator, shaped like Math.random. */
export type PRNG = () => number;

/**
 * Builds the generator used to resolve hashed (H) field values.
 *
 * The generator lives in the Go port and is reached through a handle, because
 * its state evolves between draws: redrawing a block on each crossing would
 * replay the same values forever. An empty or absent seed produces a
 * non-deterministic sequence, matching the original's treatment of a falsy
 * seed.
 */
export function seededRandom(str?: string): PRNG {
  const owner = { handle: call<number>('random.new', str ?? '') };
  trackHandle(owner, owner.handle);

  // Closing over owner keeps the Go-side generator alive for as long as the
  // returned function is reachable.
  return () => call<number>('random.next', owner.handle);
}
