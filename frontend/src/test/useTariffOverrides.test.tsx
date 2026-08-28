import { act, renderHook } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';

import { HttpUtil } from '@/utils';
import { useTariffOverrides } from '@/hooks/useTariffOverrides';

const withTariff = { email: 'test@x.com', group: 'default' };

function makeWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe('useTariffOverrides', () => {
  const wrapper = makeWrapper();

  it('marks all fields as managed when no overrides and client exists', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(withTariff, true, true), { wrapper });

    expect(result.current.isFieldManaged('totalGB')).toBe(true);
    expect(result.current.isFieldManaged('limitIP')).toBe(true);
    expect(result.current.isFieldManaged('expiryTime')).toBe(true);
    expect(result.current.isFieldManaged('inbounds')).toBe(true);
  });

  it('makeLocal un-manages a field', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(withTariff, true, true), { wrapper });

    expect(result.current.isFieldManaged('totalGB')).toBe(true);

    act(() => result.current.makeLocal('totalGB'));

    expect(result.current.isFieldManaged('totalGB')).toBe(false);
  });

  it('makeLocal removes field from removed set (idempotent)', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(withTariff, true, true), { wrapper });

    act(() => result.current.returnToTariff('totalGB'));
    expect(result.current.isFieldManaged('totalGB')).toBe(true);

    act(() => result.current.makeLocal('totalGB'));
    expect(result.current.isFieldManaged('totalGB')).toBe(false);
  });

  it('returnToTariff re-manages a previously made-local field', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(withTariff, true, true), { wrapper });

    act(() => result.current.makeLocal('limitIP'));
    expect(result.current.isFieldManaged('limitIP')).toBe(false);

    act(() => result.current.returnToTariff('limitIP'));
    expect(result.current.isFieldManaged('limitIP')).toBe(true);
  });

  it('computeDiff returns fields to override and return', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(withTariff, true, true), { wrapper });

    act(() => result.current.makeLocal('totalGB'));
    act(() => result.current.makeLocal('inbounds'));

    const diff = result.current.computeDiff();
    expect(diff.toOverride).toEqual(expect.arrayContaining(['totalGB', 'inbounds']));
    expect(diff.toReturn).toEqual([]);
  });

  it('computeDiff detects return of previously overridden field', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(withTariff, true, true), { wrapper });

    act(() => result.current.makeLocal('totalGB'));
    act(() => result.current.returnToTariff('totalGB'));

    const diff = result.current.computeDiff();
    expect(diff.toOverride).toEqual([]);
    expect(diff.toReturn).toEqual([]);
  });

  it('all fields managed when not in edit mode', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(withTariff, false, true), { wrapper });

    expect(result.current.isFieldManaged('totalGB')).toBe(true);
  });

  it('all fields managed when client is null', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result } = renderHook(() => useTariffOverrides(null, true, true), { wrapper });

    expect(result.current.isFieldManaged('totalGB')).toBe(true);
    expect(result.current.isFieldManaged('limitIP')).toBe(true);
    expect(result.current.isFieldManaged('expiryTime')).toBe(true);
    expect(result.current.isFieldManaged('inbounds')).toBe(true);
  });

  it('reset added set when modal closes', () => {
    vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, msg: '', obj: {} });
    const { result, rerender } = renderHook(
      ({ open }) => useTariffOverrides(withTariff, true, open),
      { initialProps: { open: true }, wrapper },
    );

    act(() => result.current.makeLocal('totalGB'));
    expect(result.current.isFieldManaged('totalGB')).toBe(false);

    rerender({ open: false });
    rerender({ open: true });

    expect(result.current.isFieldManaged('totalGB')).toBe(true);
  });
});
