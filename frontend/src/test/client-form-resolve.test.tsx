import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { ThemeProvider } from '@/hooks/useTheme';
import { keys } from '@/api/queryKeys';
import { HttpUtil, Msg } from '@/utils';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';

/* ---- helpers ---- */

afterEach(() => {
  // Reset HttpUtil.get mock back to default from setup.components.ts
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  vi.spyOn(HttpUtil, 'get').mockResolvedValue({ success: true, obj: {} } as any);
});

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

const VLESS_INBOUND = {
  id: 1,
  port: 443,
  protocol: 'vless',
  tag: 'in-443-tcp',
  tlsFlowCapable: true,
  enable: true,
} as unknown as InboundOption;

const VIP_GROUP = {
  name: 'vip',
  tariffId: 1,
  tariffName: 'Gold',
  tariff: {
    id: 1,
    name: 'Gold',
    trafficStrategy: 'overwrite',
    inboundStrategy: 'overwrite',
    enable: true,
  },
};

function countLocks(): number {
  return document.querySelectorAll('.anticon-lock').length;
}

/* ---- 2.5  Resolve API populates tariff values when group selected ---- */

describe('2.5 — resolve API is called and locks appear', () => {
  it('calls resolve endpoint and shows 4 lock icons', async () => {
    vi.mocked(HttpUtil.get).mockImplementation(async (url: string) => {
      if (url.includes('/clients/get/resolve/')) {
        return new Msg(true, '', { totalGB: 1073741824, limitIp: 10, expiryTime: 0, inboundIds: [1] });
      }
      return new Msg(true, '', {});
    });

    const qc = makeQC();
    qc.setQueryData(keys.clients.groups(), [VIP_GROUP]);

    render(
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <ClientFormModal
            open
            mode="edit"
            client={{
              email: 'tariffuser',
              group: 'vip',
              totalGB: 0,
              limitIp: 0,
              expiryTime: 0,
              enable: true,
              subId: 'sub1',
              totalGBIsOverridden: false,
              limitIPIsOverridden: false,
              expiryIsOverridden: false,
              isInboundsOverridden: false,
              tariffName: 'Gold',
            } as unknown as ClientRecord}
            inbounds={[VLESS_INBOUND]}
            attachedIds={[1]}
            save={vi.fn().mockResolvedValue({ success: true })}
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      </ThemeProvider>,
    );

    await screen.findByText(/save/i, {}, { timeout: 3000 });

    expect(countLocks()).toBe(4);
  });
});
