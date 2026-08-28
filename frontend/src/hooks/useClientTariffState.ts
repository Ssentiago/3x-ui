import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
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

export interface TariffFieldValues {
  totalGB: number;
  limitIp: number;
  expiryTime: number;
  inboundIds: number[];
}

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

type Field = 'totalGB' | 'limitIP' | 'expiryTime' | 'inbounds';

interface UseClientTariffStateInput {
  email?: string;
  group?: string;
  totalGB?: number;
  limitIp?: number;
  expiryTime?: number;
}

export function useClientTariffState(
  client: UseClientTariffStateInput | null | undefined,
  selectedGroup: { name: string; tariff?: unknown } | null,
  attachedIds: number[],
  isEdit: boolean,
  open: boolean,
) {
  const isManaged = !!selectedGroup?.tariff;
  const email = client?.email ?? '';

  const clientValue = useMemo<TariffFieldValues>(() => ({
    totalGB: client?.totalGB ?? 0,
    limitIp: client?.limitIp ?? 0,
    expiryTime: Number(client?.expiryTime) || 0,
    inboundIds: attachedIds,
  }), [client?.totalGB, client?.limitIp, client?.expiryTime, attachedIds]);

  const effQuery = useQuery({
    queryKey: ['clientEffective', email],
    queryFn: async () => {
      const res = await HttpUtil.get(`/panel/api/clients/get/effective/${encodeURIComponent(email)}`);
      if (!res?.success || !res.obj) return null;
      return res.obj as ClientEffectiveData;
    },
    enabled: isEdit && !!client?.group && !!email && open,
    staleTime: 0,
  });

  const groupChanged = !!(selectedGroup?.tariff && selectedGroup.name !== client?.group);
  const previewQuery = useQuery({
    queryKey: ['clientResolve', email, selectedGroup?.name],
    queryFn: async () => {
      const msg = await HttpUtil.get(
        `/panel/api/clients/get/resolve/${encodeURIComponent(email)}?group=${encodeURIComponent(selectedGroup!.name)}`,
      );
      if (!msg?.success || !msg.obj) return null;
      return msg.obj as { totalGB?: number; expiryTime?: number; limitIp?: number; inboundIds?: number[] };
    },
    enabled: isEdit && groupChanged && !!email && selectedGroup != null,
    staleTime: 30_000,
  });

  const tariffValue = useMemo<TariffFieldValues | null>(() => {
    if (groupChanged) {
      const r = previewQuery.data;
      if (!r) return null;
      return {
        totalGB: r.totalGB ?? 0,
        limitIp: r.limitIp ?? 0,
        expiryTime: r.expiryTime ?? 0,
        inboundIds: r.inboundIds ?? [],
      };
    }
    const eff = effQuery.data;
    if (!eff) return null;
    return {
      totalGB: eff.resolvedTotalGB ?? 0,
      limitIp: eff.resolvedLimitIP ?? 0,
      expiryTime: eff.resolvedExpiryTime ?? 0,
      inboundIds: eff.resolvedInboundIds ?? [],
    };
  }, [groupChanged, previewQuery.data, effQuery.data]);

  const serverOverrides = useMemo(() => {
    const eff = effQuery.data;
    if (!eff) return new Set<Field>();
    const s = new Set<Field>();
    if (eff.isTotalGBOverridden) s.add('totalGB');
    if (eff.isLimitIPOverridden) s.add('limitIP');
    if (eff.isExpiryOverridden) s.add('expiryTime');
    if (eff.isInboundsOverridden) s.add('inbounds');
    return s;
  }, [effQuery.data]);

  const [added, setAdded] = useState<Set<Field>>(new Set());
  const [removed, setRemoved] = useState<Set<Field>>(new Set());
  const prevOpenRef = useRef(open);

  useEffect(() => {
    if (open && !prevOpenRef.current) {
      setAdded(new Set());
      setRemoved(new Set());
    }
    if (!open) {
      setAdded(new Set());
      setRemoved(new Set());
    }
    prevOpenRef.current = open;
  }, [open]);

  const overrides = useMemo(() => {
    const s = new Set(serverOverrides);
    for (const f of added) s.add(f);
    for (const f of removed) s.delete(f);
    return s;
  }, [serverOverrides, added, removed]);

  const queryClient = useQueryClient();
  const prevOpenRef2 = useRef(open);
  useEffect(() => {
    if (prevOpenRef2.current && !open && email) {
      queryClient.removeQueries({ queryKey: ['clientEffective', email] });
    }
    prevOpenRef2.current = open;
  }, [open, email, queryClient]);

  const isFieldManaged = useCallback((field: Field): boolean => {
    if (!isManaged) return false;
    if (groupChanged) return true;
    return !overrides.has(field);
  }, [isManaged, groupChanged, overrides]);

  const tariffMode = useCallback((field: Field): 'tariff' | 'override' | 'own' => {
    if (!isManaged) return 'own';
    if (isFieldManaged(field)) return 'tariff';
    return 'override';
  }, [isManaged, isFieldManaged]);

  const makeLocal = useCallback((field: Field) => {
    setAdded((prev) => { const n = new Set(prev); n.add(field); return n; });
    setRemoved((prev) => { const n = new Set(prev); n.delete(field); return n; });
  }, []);

  const returnToTariff = useCallback((field: Field) => {
    setRemoved((prev) => { const n = new Set(prev); n.add(field); return n; });
    setAdded((prev) => { const n = new Set(prev); n.delete(field); return n; });
  }, []);

  const computeDiff = useCallback((): { toOverride: Field[]; toReturn: Field[] } => {
    const toOverride = [...added].filter((f) => !serverOverrides.has(f));
    const toReturn = [...removed].filter((f) => serverOverrides.has(f));
    return { toOverride, toReturn };
  }, [serverOverrides, added, removed]);

  // POST overrideField/returnToTariff for the diff, then invalidate effective cache.
  const submitFieldOps = useCallback(async (clientEmail: string) => {
    const { toOverride, toReturn } = computeDiff();
    for (const field of toOverride) {
      await HttpUtil.post('/panel/api/clients/overrideField', { email: clientEmail, field }, JSON_HEADERS);
    }
    for (const field of toReturn) {
      await HttpUtil.post('/panel/api/clients/returnToTariff', { email: clientEmail, field }, JSON_HEADERS);
    }
    if (toOverride.length > 0 || toReturn.length > 0) {
      queryClient.invalidateQueries({ queryKey: ['clientEffective', clientEmail] });
    }
  }, [computeDiff, queryClient]);

  const loading = effQuery.isLoading || previewQuery.isLoading;

  return {
    isManaged,
    tariffValue,
    clientValue,
    isFieldManaged,
    tariffMode,
    makeLocal,
    returnToTariff,
    computeDiff,
    submitFieldOps,
    loading,
  } as const;
}
