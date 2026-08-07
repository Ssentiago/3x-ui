import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ThemeProvider } from '@/hooks/useTheme';
import ClientInfoModal from '@/pages/clients/ClientInfoModal';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';

/* ---- fixtures ---- */

const VLESS_INBOUND = {
  id: 1,
  port: 443,
  protocol: 'vless',
  tag: 'in-443-vless',
  tlsFlowCapable: true,
  enable: true,
} as unknown as InboundOption;

const VMESS_INBOUND = {
  id: 2,
  port: 8080,
  protocol: 'vmess',
  tag: 'in-8080-vmess',
  tlsFlowCapable: false,
  enable: true,
} as unknown as InboundOption;

const INBOUNDS_BY_ID = { 1: VLESS_INBOUND, 2: VMESS_INBOUND };

function renderInfo(client: ClientRecord) {
  return render(
    <ThemeProvider>
      <ClientInfoModal
        open
        client={client}
        inboundsById={INBOUNDS_BY_ID}
        isOnline={false}
        onOpenChange={() => {}}
      />
    </ThemeProvider>,
  );
}

/* ---- helpers ---- */

function bodyText(): string {
  return document.body.textContent || '';
}

/* ------------------------------------------------------------------ */
/*  3.1  Client in tariff, no override — effective (tariff) values     */
/* ------------------------------------------------------------------ */

describe('ClientInfoModal — tariff client, no override', () => {
  it('shows effective tariff values', async () => {
    const client = {
      email: 'tariffuser',
      group: 'vip',
      totalGB: 53687091200, // 50 GB
      limitIp: 10,
      expiryTime: 1735689600000, // 2025-01-01
      enable: true,
      subId: 'sub1',
      inboundIds: [1],
      tariffName: 'Gold',
      totalGBIsOverridden: false,
      limitIPIsOverridden: false,
      expiryIsOverridden: false,
      isInboundsOverridden: false,
    } as unknown as ClientRecord;

    renderInfo(client);
    await screen.findByText(/client info/i, {}, { timeout: 3000 });

    const text = bodyText();
    expect(text).toContain('50.00 GB');
    expect(text).toContain('10');
    expect(text).toContain('in-443-vless');
  });
});

/* ------------------------------------------------------------------ */
/*  3.2  Client in tariff, totalGB overridden                          */
/* ------------------------------------------------------------------ */

describe('ClientInfoModal — tariff client with override', () => {
  it('shows overridden totalGB alongside tariff values for other fields', async () => {
    const client = {
      email: 'overrideuser',
      group: 'vip',
      totalGB: 21474836480, // 20 GB (overridden)
      limitIp: 10,          // tariff
      expiryTime: 1735689600000, // tariff
      enable: true,
      subId: 'sub2',
      inboundIds: [1],
      tariffName: 'Gold',
      totalGBIsOverridden: true,
      limitIPIsOverridden: false,
      expiryIsOverridden: false,
      isInboundsOverridden: false,
    } as unknown as ClientRecord;

    renderInfo(client);
    await screen.findByText(/client info/i, {}, { timeout: 3000 });

    const text = bodyText();
    expect(text).toContain('20.00 GB');
    expect(text).toContain('10');
    expect(text).toContain('in-443-vless');
  });
});

/* ------------------------------------------------------------------ */
/*  3.3  Client without tariff — raw client values                     */
/* ------------------------------------------------------------------ */

describe('ClientInfoModal — non-tariff client', () => {
  it('shows raw client values', async () => {
    const client = {
      email: 'freeuser',
      group: 'free',
      totalGB: 10737418240, // 10 GB (raw)
      limitIp: 5,
      expiryTime: 0,
      enable: true,
      subId: 'sub3',
      inboundIds: [2],
      totalGBIsOverridden: false,
      limitIPIsOverridden: false,
      expiryIsOverridden: false,
      isInboundsOverridden: false,
    } as unknown as ClientRecord;

    renderInfo(client);
    await screen.findByText(/client info/i, {}, { timeout: 3000 });

    const text = bodyText();
    expect(text).toContain('10.00 GB');
    expect(text).toContain('5');
    expect(text).toContain('in-8080-vmess');
  });
});
