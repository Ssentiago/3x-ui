import { useTranslation } from 'react-i18next';
import { Form, Input, Modal, Select } from 'antd';

interface GroupFormModalProps {
  open: boolean;
  title: string;
  saving: boolean;
  nameValue: string;
  tariffIdValue: number | null;
  onNameChange: (v: string) => void;
  onTariffIdChange: (v: number | null) => void;
  onOk: () => void;
  onCancel: () => void;
  tariffs: { id: number; name: string }[];
}

export default function GroupFormModal({
  open, title, saving, nameValue, tariffIdValue,
  onNameChange, onTariffIdChange, onOk, onCancel, tariffs,
}: GroupFormModalProps) {
  const { t } = useTranslation();

  return (
    <Modal
      open={open}
      title={title}
      okText={t('save')}
      cancelText={t('cancel')}
      confirmLoading={saving}
      onCancel={onCancel}
      onOk={onOk}
      destroyOnHidden
    >
      <Form layout="vertical">
        <Form.Item label={t('pages.groups.name')}>
          <Input
            value={nameValue}
            onChange={(e) => onNameChange(e.target.value)}
            onPressEnter={onOk}
            placeholder={t('pages.clients.groupPlaceholder')}
            autoFocus
          />
        </Form.Item>
        <Form.Item label={t('pages.tariffs.title')}>
          <Select
            value={tariffIdValue}
            onChange={onTariffIdChange}
            allowClear
            showSearch={{ optionFilterProp: 'label' }}
            placeholder={t('pages.groups.noTariff')}
            options={[
              { value: null, label: t('pages.groups.noTariff') },
              ...tariffs.map((p) => ({ value: p.id, label: p.name })),
            ]}
          />
        </Form.Item>
      </Form>
    </Modal>
  );
}
