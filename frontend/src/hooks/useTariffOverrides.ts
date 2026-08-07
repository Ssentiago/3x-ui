import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { HttpUtil } from '@/utils';

interface ClientEffectiveData {
  tariffId?: number;
  tariffName?: string;
  startedAt?: number;
  endedAt?: number | null;
  isTotalGBOverridden?: boolean;
  isLimitIPOverridden?: boolean;
  isExpiryOverridden?: boolean;
  isInboundsOverridden?: boolean;
  resolvedTotalGB?: number;
  resolvedLimitIP?: number;
  resolvedExpiryTime?: number;
  resolvedInboundIds?: number[];
}

function readClientOverrides(eff: ClientEffectiveData | null | undefined): Set<string> {
  if (!eff) return new Set();
  const s = new Set<string>();
  if (eff.isTotalGBOverridden) s.add('totalGB');
  if (eff.isLimitIPOverridden) s.add('limitIP');
  if (eff.isExpiryOverridden) s.add('expiryTime');
  if (eff.isInboundsOverridden) s.add('inbounds');
  return s;
}

interface UseTariffOverridesInput {
  email?: string;
  group?: string;
}

export function useTariffOverrides(client: UseTariffOverridesInput | null | undefined, isEdit: boolean, open: boolean) {
  const hasTariff = !!client?.group;
  const email = client?.email ?? '';

  const effQuery = useQuery({
    queryKey: ['clientEffective', email],
    queryFn: async () => {
      const res = await HttpUtil.get(`/panel/api/client/get/effective/${encodeURIComponent(email)}`);
      if (!res || (typeof res === 'object' && Object.keys(res).length === 0)) return null;
      return res as ClientEffectiveData;
    },
    enabled: isEdit && hasTariff && !!email && open,
    staleTime: 30_000,
  });

  const clientOverrides = useMemo(() => readClientOverrides(effQuery.data), [effQuery.data]);

  const [added, setAdded] = useState<Set<string>>(new Set());
  const [removed, setRemoved] = useState<Set<string>>(new Set());
  const prevOpenRef = useRef(open);

  useEffect(() => {
    if (open && !prevOpenRef.current) {
      setAdded(new Set());
    }
    if (!open) {
      setAdded(new Set());
    }
    prevOpenRef.current = open;
  }, [open]);

  const effective = useMemo(() => {
    const s = new Set(clientOverrides);
    for (const f of added) s.add(f);
    for (const f of removed) s.delete(f);
    return s;
  }, [clientOverrides, added, removed]);

  const isFieldManaged = useCallback((field: string) => {
    return !effective.has(field);
  }, [effective]);

  const makeLocal = useCallback((field: string) => {
    setAdded((prev) => {
      const next = new Set(prev);
      next.add(field);
      return next;
    });
    setRemoved((prev) => {
      const next = new Set(prev);
      next.delete(field);
      return next;
    });
  }, []);

  const returnToTariff = useCallback((field: string) => {
    setRemoved((prev) => {
      const next = new Set(prev);
      next.add(field);
      return next;
    });
    setAdded((prev) => {
      const next = new Set(prev);
      next.delete(field);
      return next;
    });
  }, []);

  const computeDiff = useCallback(() => {
    const toOverride = [...added].filter((f) => !clientOverrides.has(f));
    const toReturn = [...removed].filter((f) => clientOverrides.has(f));
    return { toOverride, toReturn };
  }, [clientOverrides, added, removed]);

  return { isFieldManaged, makeLocal, returnToTariff, computeDiff };
}
