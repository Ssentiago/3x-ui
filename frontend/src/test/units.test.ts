import { describe, expect, it } from 'vitest';

import { bytesToGB, gbToBytes } from '@/lib/clients/units';

describe('bytesToGB', () => {
  it('converts bytes to GB', () => {
    expect(bytesToGB(1073741824)).toBe(1);
  });

  it('returns 0 for 0 bytes', () => {
    expect(bytesToGB(0)).toBe(0);
  });

  it('returns 0 for negative bytes', () => {
    expect(bytesToGB(-1)).toBe(0);
  });

  it('rounds to 2 decimals', () => {
    // 1.5 GB = 1.5 * 2^30 = 1610612736
    expect(bytesToGB(1610612736)).toBe(1.5);
  });
});

describe('gbToBytes', () => {
  it('converts GB to bytes', () => {
    expect(gbToBytes(1)).toBe(1073741824);
  });

  it('returns 0 for 0 GB', () => {
    expect(gbToBytes(0)).toBe(0);
  });

  it('returns 0 for undefined', () => {
    expect(gbToBytes(undefined)).toBe(0);
  });

  it('returns 0 for negative GB', () => {
    expect(gbToBytes(-5)).toBe(0);
  });

  it('rounds to integer bytes', () => {
    // 0.5 GB = 536870912 bytes
    expect(gbToBytes(0.5)).toBe(536870912);
  });
});
