import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Tag, Tooltip } from 'antd';
import ManagedField from '@/components/ManagedField';

interface TariffManagedFieldProps {
  field: string;
  isFieldManaged: boolean;
  hasTariff: boolean;
  tariffName: string;
  onMakeLocal: () => void;
  onReturnToTariff: () => void;
  children: ReactNode;
}

export default function TariffManagedField({
  field,
  isFieldManaged,
  hasTariff,
  tariffName,
  onMakeLocal,
  onReturnToTariff,
  children,
}: TariffManagedFieldProps) {
  const { t } = useTranslation();

  if (!hasTariff) return <>{children}</>;

  if (isFieldManaged) {
    return (
      <ManagedField managed tariffName={tariffName} onMakeLocal={onMakeLocal}>
        {children}
      </ManagedField>
    );
  }

  return (
    <>
      {children}
      <Tooltip title={t('pages.clients.returnToTariffDesc', { field: t(`pages.clients.${field === 'expiryTime' ? 'expireDays' : field}`) })}>
        <Button size="small" type="link" onClick={onReturnToTariff}>
          {t('pages.clients.returnToTariff')}
        </Button>
      </Tooltip>
    </>
  );
}

export function TariffTag({ show, tariffName }: { show: boolean; tariffName: string }) {
  if (!show) return null;
  return (
    <Tag color="blue" style={{ marginLeft: 4, fontSize: 10 }}>
      {tariffName}
    </Tag>
  );
}
