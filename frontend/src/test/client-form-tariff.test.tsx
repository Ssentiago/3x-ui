import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { ThemeProvider } from '@/hooks/useTheme';
import { keys } from '@/api/queryKeys';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';

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

describe('ClientFormModal tariff flow', () => {
  it('shows lock icons when client is in tariff group', async () => {
    const qc = makeQC();

    qc.setQueryData(keys.clients.groups(), [
      {
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
      },
    ]);

    const client = {
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
    } as unknown as ClientRecord;

    const save = vi.fn().mockResolvedValue({ success: true });

    render(
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <ClientFormModal
            open
            mode="edit"
            client={client}
            inbounds={[VLESS_INBOUND]}
            attachedIds={[1]}
            save={save}
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      </ThemeProvider>,
    );

    await screen.findByText(/save/i, {}, { timeout: 3000 });

    const lockIcons = document.querySelectorAll('.anticon-lock');
    expect(lockIcons.length).toBeGreaterThan(0);
  });

  it('does not show lock icons for non-tariff client', async () => {
    const qc = makeQC();

    qc.setQueryData(keys.clients.groups(), [
      { name: 'free', tariffId: null, tariffName: null, tariff: null },
    ]);

    const client = {
      email: 'freeuser',
      group: 'free',
      totalGB: 100,
      limitIp: 5,
      expiryTime: 0,
      enable: true,
      subId: 'sub2',
      totalGBIsOverridden: false,
      limitIPIsOverridden: false,
      expiryIsOverridden: false,
      isInboundsOverridden: false,
    } as unknown as ClientRecord;

    const save = vi.fn().mockResolvedValue({ success: true });

    render(
      <ThemeProvider>
        <QueryClientProvider client={qc}>
          <ClientFormModal
            open
            mode="edit"
            client={client}
            inbounds={[VLESS_INBOUND]}
            attachedIds={[1]}
            save={save}
            onOpenChange={() => {}}
          />
        </QueryClientProvider>
      </ThemeProvider>,
    );

    await screen.findByText(/save/i, {}, { timeout: 3000 });

    const lockIcons = document.querySelectorAll('.anticon-lock');
    expect(lockIcons.length).toBe(0);
  });
});
