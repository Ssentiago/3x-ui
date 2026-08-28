import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

interface UseInboundOverrideInput {
  isInboundsManaged: boolean;
  tariffInboundIds: number[];
  ownInboundIds: number[];
  isManaged: boolean;
}

export function useInboundOverride({
  isInboundsManaged,
  tariffInboundIds,
  ownInboundIds,
  isManaged,
}: UseInboundOverrideInput) {
  const [overrideIds, setOverrideIds] = useState<number[]>([]);

  const prevManagedRef = useRef(isInboundsManaged);
  useEffect(() => {
    if (prevManagedRef.current && !isInboundsManaged && overrideIds.length === 0) {
      setOverrideIds([...tariffInboundIds]);
    }
    prevManagedRef.current = isInboundsManaged;
  }, [isInboundsManaged, tariffInboundIds]);

  const effectiveInboundIds = useMemo(() => {
    if (!isManaged) return ownInboundIds;
    if (isInboundsManaged) return tariffInboundIds;
    return overrideIds.length > 0 ? overrideIds : tariffInboundIds;
  }, [isManaged, isInboundsManaged, ownInboundIds, tariffInboundIds, overrideIds]);

  const isOverridden = isManaged && !isInboundsManaged;

  const setOverrideInboundIds = useCallback((ids: number[]) => {
    setOverrideIds(ids);
  }, []);

  const resetToTariff = useCallback(() => {
    setOverrideIds([]);
  }, []);

  return { effectiveInboundIds, setOverrideInboundIds, isOverridden, resetToTariff } as const;
}
