import { describe, expect, it, vi } from 'vitest';
import { screen, fireEvent } from '@testing-library/react';

import ManagedField from '@/components/ManagedField';
import { renderWithProviders } from './test-utils';

describe('ManagedField', () => {
  it('renders children directly when not managed', () => {
    renderWithProviders(
      <ManagedField managed={false} tariffName="Gold" onMakeLocal={() => {}}>
        <input data-testid="child-input" />
      </ManagedField>,
    );
    expect(screen.getByTestId('child-input')).toBeTruthy();
    expect(document.querySelector('.anticon-lock')).toBeFalsy();
  });

  it('shows lock icon when managed', () => {
    renderWithProviders(
      <ManagedField managed={true} tariffName="Gold" onMakeLocal={() => {}}>
        <input data-testid="child-input" />
      </ManagedField>,
    );
    expect(document.querySelector('.anticon-lock')).toBeTruthy();
  });

  it('clicking lock opens popover with make local button', async () => {
    const onMakeLocal = vi.fn();
    renderWithProviders(
      <ManagedField managed={true} tariffName="Gold" onMakeLocal={onMakeLocal}>
        <input data-testid="child-input" />
      </ManagedField>,
    );

    const wrapper = document.querySelector('[style*="position: relative"]') as HTMLElement;
    fireEvent.click(wrapper);

    const btn = await screen.findByRole('button', { name: /make local/i });
    fireEvent.click(btn);
    expect(onMakeLocal).toHaveBeenCalled();
  });
});
