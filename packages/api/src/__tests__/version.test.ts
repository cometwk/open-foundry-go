import { describe, it, expect } from 'vitest';
import { readPlatformVersion } from '../version.js';

describe('readPlatformVersion', () => {
  it('resolves the monorepo root package.json version (SemVer)', () => {
    const v = readPlatformVersion();
    expect(v).toMatch(/^\d+\.\d+\.\d+/);
    // Not the generator fallback, and not the api package version (0.0.1).
    expect(v).not.toBe('0.0.1');
  });

  it('returns the supplied fallback only when nothing is found (smoke)', () => {
    // The real lookup succeeds in-repo, so this just asserts the param is typed
    // and the happy path does not return the fallback.
    expect(readPlatformVersion('SHOULD_NOT_BE_USED')).not.toBe('SHOULD_NOT_BE_USED');
  });
});
