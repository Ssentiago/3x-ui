import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button, Popover, theme, Typography } from 'antd';
import { LockOutlined } from '@ant-design/icons';
import type { ReactNode } from 'react';

interface ManagedFieldProps {
  managed: boolean;
  tariffName: string;
  onMakeLocal: () => void;
  children: ReactNode;
}

export default function ManagedField(props: ManagedFieldProps) {
  const { managed, tariffName, onMakeLocal, children } = props;
  const { t } = useTranslation();
  const { token } = theme.useToken();
  const [hovered, setHovered] = useState(false);

  if (!managed) return <>{children}</>;

  return (
    <Popover
      trigger="click"
      content={
        <div style={{ maxWidth: 260 }}>
          <Typography.Text strong>
            <LockOutlined style={{ marginRight: 6 }} />
            {t('pages.clients.managedFieldLocked', { name: tariffName })}
          </Typography.Text>
          <br />
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {t('pages.clients.managedFieldLockedDesc')}
          </Typography.Text>
          <div style={{ marginTop: 10 }}>
            <Button size="small" type="primary" block onClick={onMakeLocal}>
              {t('pages.clients.makeLocal')}
            </Button>
          </div>
        </div>
      }
    >
      <div
        style={{ position: 'relative', cursor: 'pointer' }}
        onMouseEnter={() => setHovered(true)}
        onMouseLeave={() => setHovered(false)}
      >
        <div style={{ pointerEvents: 'none' }}>
          {children}
        </div>
        <div
          style={{
            position: 'absolute',
            inset: 0,
            background: token.colorFill,
            backdropFilter: hovered ? 'blur(3px)' : 'none',
            WebkitBackdropFilter: hovered ? 'blur(3px)' : 'none',
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            borderRadius: token.borderRadius,
            opacity: hovered ? 1 : 0,
            transition: 'opacity 0.15s, backdrop-filter 0.15s',
            pointerEvents: 'none',
            gap: 4,
          }}
        >
          <LockOutlined style={{ fontSize: 16, color: token.colorText }} />
        </div>
      </div>
    </Popover>
  );
}
