import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import { ThemeProvider } from '@/hooks/useTheme';
import TariffFormModal from '@/pages/groups/TariffFormModal';
import ProfileFormModal from '@/pages/groups/ProfileFormModal';
import GroupFormModal from '@/pages/groups/GroupFormModal';

function makeQC() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function renderWithTheme(ui: React.ReactElement) {
  const qc = makeQC();
  return render(
    <ThemeProvider>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </ThemeProvider>,
  );
}

describe('TariffFormModal', () => {
  it('renders create mode with name input', () => {
    renderWithTheme(
      <TariffFormModal
        open
        editingTariff={null}
        profiles={[]}
        inboundLabelById={new Map()}
        saving={false}
        onClose={() => {}}
        onSave={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(screen.getByTitle('Name')).toBeTruthy();
    expect(screen.getByText('Create')).toBeTruthy();
    expect(screen.getByText('Close')).toBeTruthy();
  });

  it('renders readonly mode without save button', () => {
    renderWithTheme(
      <TariffFormModal
        open
        editingTariff={{
          id: 1,
          name: 'Gold',
          trafficStrategy: 'overwrite',
          inboundStrategy: 'overwrite',
          enable: true,
          groupCount: 0,
        }}
        profiles={[]}
        inboundLabelById={new Map()}
        saving={false}
        readonly
        onClose={() => {}}
      />,
    );

    expect(screen.queryByText('Create')).toBeFalsy();
  });

  it('shows empty chain message when no profiles', () => {
    renderWithTheme(
      <TariffFormModal
        open
        editingTariff={null}
        profiles={[]}
        inboundLabelById={new Map()}
        saving={false}
        onClose={() => {}}
        onSave={vi.fn().mockResolvedValue(undefined)}
      />,
    );

    expect(screen.getByText(/No profiles in the chain/i)).toBeTruthy();
  });
});

describe('ProfileFormModal', () => {
  it('renders create mode with all fields', () => {
    renderWithTheme(
      <ProfileFormModal
        open
        editingProfile={null}
        inboundOptions={[]}
        saving={false}
        onCancel={() => {}}
        onOk={vi.fn()}
      />,
    );

    expect(screen.getByTitle('Name')).toBeTruthy();
    expect(screen.getByText(/Traffic/i)).toBeTruthy();
    expect(screen.getByText(/Expiry/i)).toBeTruthy();
    expect(screen.getByText('Create')).toBeTruthy();
  });

  it('renders edit mode with prefilled name', () => {
    renderWithTheme(
      <ProfileFormModal
        open
        editingProfile={{
          id: 1,
          name: 'BASE',
          traffic: 100,
          expiryDays: 30,
          limitIp: 3,
          inboundIds: [],
          tariffCount: 0,
        }}
        inboundOptions={[]}
        saving={false}
        onCancel={() => {}}
        onOk={vi.fn()}
      />,
    );

    const nameInput = screen.getByDisplayValue('BASE');
    expect(nameInput).toBeTruthy();
  });
});

describe('GroupFormModal', () => {
  it('renders with name input and tariff select', () => {
    renderWithTheme(
      <GroupFormModal
        open
        title="Create Group"
        nameValue=""
        tariffIdValue={null}
        onNameChange={() => {}}
        onTariffIdChange={() => {}}
        tariffs={[]}
        saving={false}
        onCancel={() => {}}
        onOk={() => {}}
      />,
    );

    expect(screen.getByTitle('Name')).toBeTruthy();
    expect(screen.getByTitle('Tariff')).toBeTruthy();
  });
});
