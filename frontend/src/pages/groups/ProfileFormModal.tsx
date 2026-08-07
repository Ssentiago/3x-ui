import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { Col, Input, InputNumber, Modal, Row, Select } from 'antd';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';

import { ProfileFormSchema, type Profile, type ProfileFormValues } from '@/schemas/profile';
import { FormField } from '@/components/form/rhf';

interface ProfileFormModalProps {
  open: boolean;
  editingProfile: Profile | null;
  saving: boolean;
  inboundOptions: { value: number; label: string }[];
  onOk: (values: ProfileFormValues) => void;
  onCancel: () => void;
}

export default function ProfileFormModal({ open, editingProfile, saving, inboundOptions, onOk, onCancel }: ProfileFormModalProps) {
  const { t } = useTranslation();
  const methods = useForm<ProfileFormValues>({
    resolver: zodResolver(ProfileFormSchema),
    defaultValues: { name: '', traffic: null, expiryDays: null, limitIp: null, inboundIds: [] },
  });

  useEffect(() => {
    if (!open) return;
    if (editingProfile) {
      methods.reset({
        name: editingProfile.name,
        traffic: editingProfile.traffic,
        expiryDays: editingProfile.expiryDays,
        limitIp: editingProfile.limitIp,
        inboundIds: editingProfile.inboundIds || [],
      });
    } else {
      methods.reset({ name: '', traffic: null, expiryDays: null, limitIp: null, inboundIds: [] });
    }
  }, [open, editingProfile, methods]);

  return (
    <Modal
      open={open}
      title={editingProfile ? t('pages.profiles.editProfile') : t('pages.profiles.addProfile')}
      okText={editingProfile ? t('pages.clients.submitEdit') : t('create')}
      cancelText={t('close')}
      okButtonProps={{ loading: saving }}
      onOk={methods.handleSubmit(onOk)}
      onCancel={onCancel}
      width={600}
    >
      <FormProvider {...methods}>
        <form>
          <FormField name="name" label={t('pages.profiles.name')} rules={{ required: t('pages.profiles.name') }}>
            <Input placeholder={t('pages.profiles.namePlaceholder')} />
          </FormField>
          <Row gutter={16}>
            <Col span={12}>
              <FormField name="traffic" label={t('pages.profiles.traffic')}>
                <InputNumber min={0} style={{ width: '100%' }} placeholder={t('pages.tariffs.unlimitedPlaceholder')} />
              </FormField>
            </Col>
            <Col span={12}>
              <FormField name="expiryDays" label={t('pages.profiles.expiryDays')}>
                <InputNumber min={0} style={{ width: '100%' }} placeholder={t('pages.tariffs.neverPlaceholder')} />
              </FormField>
            </Col>
          </Row>
          <Row gutter={16}>
            <Col span={12}>
              <FormField name="limitIp" label={t('pages.profiles.limitIp')}>
                <InputNumber min={0} style={{ width: '100%' }} placeholder={t('pages.tariffs.unlimitedPlaceholder')} />
              </FormField>
            </Col>
          </Row>
          <FormField name="inboundIds" label={t('pages.profiles.inbounds')}>
            <Select mode="multiple" placeholder={t('pages.profiles.inbounds')} options={inboundOptions} showSearch={{ optionFilterProp: 'label' }} />
          </FormField>
        </form>
      </FormProvider>
    </Modal>
  );
}
