import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Modal,
  Space,
  Table,
  Tag,
  Tooltip,
} from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons';
import { useMutation } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import type { Profile, ProfileFormValues } from '@/schemas/profile';
import ProfileFormModal from './ProfileFormModal';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface ProfilesTabProps {
  profiles: Profile[];
  loading: boolean;
  inboundOptions: { value: number; label: string }[];
  inboundLabelById: Map<number, string>;
  invalidate: () => void;
}

export default function ProfilesTab({ profiles, loading, inboundOptions, inboundLabelById, invalidate }: ProfilesTabProps) {
  const { t } = useTranslation();
  const [modal, modalContextHolder] = Modal.useModal();

  const [profileModalOpen, setProfileModalOpen] = useState(false);
  const [editingProfile, setEditingProfile] = useState<Profile | null>(null);

  const profileCreateMut = useMutation({
    mutationFn: (body: ProfileFormValues) =>
      HttpUtil.post('/panel/api/clients/profiles/create', { name: body.name, traffic: body.traffic ?? null, expiryDays: body.expiryDays ?? null, limitIp: body.limitIp ?? null, inboundIds: body.inboundIds || [] }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const profileUpdateMut = useMutation({
    mutationFn: (body: { id: number } & ProfileFormValues) =>
      HttpUtil.post(`/panel/api/clients/profiles/${body.id}/update`, { name: body.name, traffic: body.traffic ?? null, expiryDays: body.expiryDays ?? null, limitIp: body.limitIp ?? null, inboundIds: body.inboundIds || [] }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const profileDeleteMut = useMutation({
    mutationFn: (id: number) =>
      HttpUtil.post(`/panel/api/clients/profiles/${id}/delete`, undefined, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  function openCreate() {
    setEditingProfile(null);
    setProfileModalOpen(true);
  }

  function openEdit(row: Profile) {
    setEditingProfile(row);
    setProfileModalOpen(true);
  }

  async function handleOk(values: ProfileFormValues) {
    if (editingProfile) {
      await profileUpdateMut.mutateAsync({ id: editingProfile.id, ...values });
    } else {
      await profileCreateMut.mutateAsync(values);
    }
    setProfileModalOpen(false);
  }

  const tableColumns = [
    {
      title: t('pages.profiles.name'),
      dataIndex: 'name',
      key: 'name',
      width: 160,
      render: (name: string) => <Tag color="geekblue" style={{ margin: 0 }}>{name}</Tag>,
    },
    {
      title: t('pages.profiles.traffic'),
      dataIndex: 'traffic',
      key: 'traffic',
      width: 120,
      render: (v: number | null) => v === null ? '∞' : `${v} GB`,
    },
    {
      title: t('pages.profiles.expiryDays'),
      dataIndex: 'expiryDays',
      key: 'expiryDays',
      width: 120,
      render: (v: number | null) => v === null ? t('pages.tariffs.never') : `${v} ${t('days')}`,
    },
    {
      title: t('pages.profiles.limitIp'),
      dataIndex: 'limitIp',
      key: 'limitIp',
      width: 100,
      render: (v: number | null) => v === null ? '∞' : v,
    },
    {
      title: t('pages.profiles.inbounds'),
      dataIndex: 'inboundIds',
      key: 'inboundIds',
      width: 200,
      render: (ids: number[]) => {
        if (!ids.length) return <span style={{ color: '#999' }}>{t('pages.groups.noTariff')}</span>;
        const labels = ids.map((id) => inboundLabelById.get(id) ?? String(id));
        const rest = labels.length > 2 ? ` +${labels.length - 2}` : '';
        return (
          <Tooltip title={labels.join(', ')}>
            <span>{labels.slice(0, 2).join(', ')}{rest}</span>
          </Tooltip>
        );
      },
    },
    {
      title: t('pages.profiles.usedByTariffs'),
      dataIndex: 'tariffCount',
      key: 'tariffCount',
      width: 120,
      render: (v: number) => v || 0,
    },
    {
      title: '',
      key: 'actions',
      width: 80,
      render: (_v: unknown, row: Profile) => (
        <Space size={4}>
          <Button size="small" type="text" icon={<EditOutlined />} onClick={() => openEdit(row)} />
          <Button size="small" type="text" danger icon={<DeleteOutlined />}
            onClick={() => {
              modal.confirm({
                title: t('pages.profiles.deleteConfirmTitle', { name: row.name }),
                content: t('pages.profiles.deleteConfirmContent'),
                okText: t('delete'),
                okType: 'danger',
                cancelText: t('cancel'),
                onOk: () => profileDeleteMut.mutate(row.id),
              });
            }} />
        </Space>
      ),
    },
  ];

  return (
    <>
      {modalContextHolder}
      <div className="card-toolbar" style={{ marginBottom: 12 }}>
        <Button aria-label={t('pages.profiles.addProfile')} type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          {t('pages.profiles.addProfile')}
        </Button>
      </div>
      {profiles.length === 0 && !loading ? (
        <div className="card-empty">
          <SafetyCertificateOutlined style={{ fontSize: 32, marginBottom: 8 }} />
          <div>{t('pages.profiles.emptyDesc')}</div>
        </div>
      ) : (
        <Table<Profile>
          dataSource={profiles}
          rowKey="id"
          size="small"
          pagination={false}
          scroll={{ x: 700 }}
          loading={loading}
          columns={tableColumns}
        />
      )}
      <ProfileFormModal
        open={profileModalOpen}
        editingProfile={editingProfile}
        saving={profileCreateMut.isPending || profileUpdateMut.isPending}
        inboundOptions={inboundOptions}
        onOk={handleOk}
        onCancel={() => setProfileModalOpen(false)}
      />
    </>
  );
}
