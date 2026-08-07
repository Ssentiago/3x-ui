import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { ThemeProvider } from '@/hooks/useTheme';
import { keys } from '@/api/queryKeys';
import { HttpUtil } from '@/utils';
import ClientFormModal from '@/pages/clients/ClientFormModal';
import type { ClientRecord, InboundOption } from '@/hooks/useClients';
import type { GroupSummary } from '@/schemas/client';

/* ------------------------------------------------------------------ */
/*  Shared infrastructure for ClientFormModal tariff tests             */
/* ------------------------------------------------------------------ */

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

/* ---- fixtures ---- */

const VLESS_INBOUND = {
  id: 1,
  port: 443,
  protocol: 'vless',
  tag: 'in-443-tcp',
  tlsFlowCapable: true,
  enable: true,
} as unknown as InboundOption;

const VIP_GROUP: GroupSummary = {
  name: 'vip',
  clientCount: 1,
  trafficUsed: 0,
  up: 0,
  down: 0,
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

const FREE_GROUP: GroupSummary = {
  name: 'free',
  clientCount: 0,
  trafficUsed: 0,
  up: 0,
  down: 0,
  tariffId: null,
  tariff: null,
};

/** Client in vip/Gold — no overrides */
function makeTariffClient(overrides: Partial<ClientRecord> = {}): ClientRecord {
  return {
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
    ...overrides,
  } as unknown as ClientRecord;
}

/** Client in free group — no tariff */
function makeFreeClient(overrides: Partial<ClientRecord> = {}): ClientRecord {
  return {
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
    ...overrides,
  } as unknown as ClientRecord;
}

function countLocks(): number {
  return document.querySelectorAll('.anticon-lock').length;
}

/* ---- render wrapper ---- */

interface RenderFormOpts {
  client: ClientRecord;
  groups?: GroupSummary[];
  save?: (...args: unknown[]) => Promise<{ success: boolean } | null>;
  mode?: 'edit' | 'add';
  inbounds?: InboundOption[];
  attachedIds?: number[];
  /** spy that intercepts HttpUtil.get for resolve and returns tariff values */
  resolveValues?: Record<string, unknown>;
}

function renderForm({
  client,
  groups = [VIP_GROUP],
  save = vi.fn<(...args: unknown[]) => Promise<{ success: boolean } | null>>().mockResolvedValue({ success: true }),
  mode = 'edit',
  inbounds = [VLESS_INBOUND],
  attachedIds = [1],
  resolveValues,
}: RenderFormOpts) {
  const qc = makeQC();
  qc.setQueryData(keys.clients.groups(), groups);

  vi.spyOn(HttpUtil, 'get').mockImplementation(async (url: string) => {
    const urlStr = String(url);
    if (urlStr.includes(`/client/get/effective/${encodeURIComponent(client.email)}`)) {
      return {
        isTotalGBOverridden: !!(client as Record<string, unknown>).totalGBIsOverridden,
        isLimitIPOverridden: !!(client as Record<string, unknown>).limitIPIsOverridden,
        isExpiryOverridden: !!(client as Record<string, unknown>).expiryIsOverridden,
        isInboundsOverridden: !!(client as Record<string, unknown>).isInboundsOverridden,
      };
    }
    if (resolveValues && urlStr.includes('/clients/get/resolve/')) {
      return resolveValues;
    }
    return {};
  });

  render(
    <ThemeProvider>
      <QueryClientProvider client={qc}>
        <ClientFormModal
          open
          mode={mode}
          client={client}
          inbounds={inbounds}
          attachedIds={attachedIds}
          save={save}
          onOpenChange={() => {}}
        />
      </QueryClientProvider>
    </ThemeProvider>,
  );

  return { qc, save };
}

/* ------------------------------------------------------------------ */
/*  2.1  Client in tariff, no override                                 */
/* ------------------------------------------------------------------ */

describe('ClientFormModal — open with tariff client, no override', () => {
  it('shows 4 lock icons for managed fields', async () => {
    renderForm({ client: makeTariffClient() });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    expect(countLocks()).toBe(4);
  });

  it('shows tariff name tag on managed fields', async () => {
    renderForm({ client: makeTariffClient() });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    const tags = document.querySelectorAll('.ant-tag');
    const goldTags = Array.from(tags).filter((t) => t.textContent?.trim() === 'Gold');
    expect(goldTags.length).toBeGreaterThan(0);
  });

  it('disables managed input fields', async () => {
    renderForm({ client: makeTariffClient() });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    const inputs = document.querySelectorAll('input[disabled]');
    expect(inputs.length).toBeGreaterThanOrEqual(2);
  });
});

/* ------------------------------------------------------------------ */
/*  2.2  Client NOT in tariff                                          */
/* ------------------------------------------------------------------ */

describe('ClientFormModal — open with non-tariff client', () => {
  it('shows zero lock icons', async () => {
    renderForm({ client: makeFreeClient(), groups: [FREE_GROUP] });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    expect(countLocks()).toBe(0);
  });

  it('does not show tariff name tag', async () => {
    renderForm({ client: makeFreeClient(), groups: [FREE_GROUP] });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    const tags = document.querySelectorAll('.ant-tag');
    const goldTags = Array.from(tags).filter((t) => t.textContent?.trim() === 'Gold');
    expect(goldTags.length).toBe(0);
  });
});

/* ------------------------------------------------------------------ */
/*  2.3  Client in tariff, totalGB overridden                          */
/* ------------------------------------------------------------------ */

describe('ClientFormModal — tariff client with totalGB override', () => {
  it('shows 3 lock icons (totalGB is local)', async () => {
    renderForm({
      client: makeTariffClient({
        totalGB: 200,
        totalGBIsOverridden: true,
      }),
    });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    expect(countLocks()).toBe(3);
  });

  it('shows Return to tariff button for overridden totalGB', async () => {
    renderForm({
      client: makeTariffClient({
        totalGB: 200,
        totalGBIsOverridden: true,
      }),
    });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    expect(screen.queryByText(/return to tariff/i)).toBeTruthy();
  });
});

/* ------------------------------------------------------------------ */
/*  2.4  Clear group — tariff notice vanishes, values → raw           */
/* ------------------------------------------------------------------ */

describe('ClientFormModal — clear group field', () => {
  it('hides tariff-managed notice when group is cleared', async () => {
    renderForm({
      client: makeTariffClient(),
      groups: [VIP_GROUP, FREE_GROUP],
    });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    const text = document.body.textContent || '';
    expect(text).toContain('Using tariff');

    const clearBtn = document.querySelector('.ant-select-clear');
    expect(clearBtn).toBeTruthy();
    fireEvent.mouseDown(clearBtn!);

    await waitFor(() => {
      const updated = document.body.textContent || '';
      expect(updated).not.toContain('Using tariff');
    });
  });
});

/* ------------------------------------------------------------------ */
/*  2.6  Save: client in tariff → tariff mode flags                    */
/* ------------------------------------------------------------------ */

describe('ClientFormModal — save with tariff client', () => {
  it('sends totalGBMode=tariff when client is managed by tariff', async () => {
    const save = vi.fn().mockResolvedValue({ success: true });
    renderForm({ client: makeTariffClient(), save });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    fireEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => expect(save).toHaveBeenCalled());

    const payload = save.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.totalGBMode).toBe('tariff');
    expect(payload.limitIpMode).toBe('tariff');
    expect(payload.expiryTimeMode).toBe('tariff');
  });

  it('sends totalGBMode=own when client has no tariff', async () => {
    const save = vi.fn().mockResolvedValue({ success: true });
    renderForm({ client: makeFreeClient(), groups: [FREE_GROUP], save });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    fireEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => expect(save).toHaveBeenCalled());

    const payload = save.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.totalGBMode).toBe('own');
    expect(payload.limitIpMode).toBe('own');
    expect(payload.expiryTimeMode).toBe('own');
  });

  it('sends totalGBMode=override when field is overridden', async () => {
    const save = vi.fn().mockResolvedValue({ success: true });
    renderForm({
      client: makeTariffClient({ totalGB: 200, totalGBIsOverridden: true }),
      save,
    });
    await screen.findByText(/save/i, {}, { timeout: 10000 });

    fireEvent.click(screen.getByRole('button', { name: /save/i }));

    await waitFor(() => expect(save).toHaveBeenCalled());

    const payload = save.mock.calls[0][0] as Record<string, unknown>;
    expect(payload.totalGBMode).toBe('override');
    expect(payload.limitIpMode).toBe('tariff');
    expect(payload.expiryTimeMode).toBe('tariff');
  });
});
