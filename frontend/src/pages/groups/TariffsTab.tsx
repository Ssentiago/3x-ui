import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Modal,
  Space,
  Table,
  Tag,
} from 'antd';
import {
  DeleteOutlined,
  EditOutlined,
  InfoCircleOutlined,
  PlusOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons';
import { useMutation } from '@tanstack/react-query';

import { HttpUtil } from '@/utils';
import { type Tariff, type TariffFormValues } from '@/schemas/tariff';
import type { Profile } from '@/schemas/profile';
import TariffFormModal from './TariffFormModal';

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface TariffsTabProps {
  tariffs: Tariff[];
  loading: boolean;
  profiles: Profile[];
  inboundLabelById: Map<number, string>;
  invalidate: () => void;
}

export default function TariffsTab({ tariffs, loading, profiles, inboundLabelById, invalidate }: TariffsTabProps) {
  const { t } = useTranslation();
  const [modal, modalContextHolder] = Modal.useModal();

  const [tariffModalOpen, setTariffModalOpen] = useState(false);
  const [editingTariff, setEditingTariff] = useState<Tariff | null>(null);
  const [viewingTariff, setViewingTariff] = useState<Tariff | null>(null);

  const tariffCreateMut = useMutation({
    mutationFn: (body: TariffFormValues) =>
      HttpUtil.post('/panel/api/clients/tariffs/create', { name: body.name, trafficStrategy: body.trafficStrategy, inboundStrategy: body.inboundStrategy }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const tariffUpdateMut = useMutation({
    mutationFn: (body: { id: number } & TariffFormValues) =>
      HttpUtil.post(`/panel/api/clients/tariffs/${body.id}/update`, { name: body.name, trafficStrategy: body.trafficStrategy, inboundStrategy: body.inboundStrategy }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const tariffDeleteMut = useMutation({
    mutationFn: (id: number) =>
      HttpUtil.post(`/panel/api/clients/tariffs/${id}/delete`, undefined, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const tariffSetProfilesMut = useMutation({
    mutationFn: (body: { id: number; profileIds: { id: number; position: number }[] }) =>
      HttpUtil.post(`/panel/api/clients/tariffs/${body.id}/profiles`, { profileIds: body.profileIds }, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  async function handleTariffSave(
    values: TariffFormValues & { tariffId?: number },
    chain: { id: number; position: number }[],
  ) {
    let tariffId = values.tariffId;
    if (!tariffId) {
      const msg = await tariffCreateMut.mutateAsync({
        name: values.name,
        trafficStrategy: values.trafficStrategy,
        inboundStrategy: values.inboundStrategy,
      });
      tariffId = (msg?.obj as Tariff)?.id;
    } else {
      await tariffUpdateMut.mutateAsync({
        id: tariffId,
        name: values.name,
        trafficStrategy: values.trafficStrategy,
        inboundStrategy: values.inboundStrategy,
      });
    }
    if (tariffId) {
      await tariffSetProfilesMut.mutateAsync({ id: tariffId, profileIds: chain });
    }
  }

  const strategyLabel: Record<string, string> = {
    overwrite: t('pages.tariffs.overwrite'),
    sum: t('pages.tariffs.sum'),
    union: t('pages.tariffs.union'),
  };

  return (
    <>
      {modalContextHolder}
      <div className="card-toolbar" style={{ marginBottom: 12 }}>
        <Button aria-label={t('pages.tariffs.addTariff')} type="primary" icon={<PlusOutlined />} onClick={() => {
          setEditingTariff(null);
          setTariffModalOpen(true);
        }}>
          {t('pages.tariffs.addTariff')}
        </Button>
      </div>
      {tariffs.length === 0 && !loading ? (
        <div className="card-empty">
          <SafetyCertificateOutlined style={{ fontSize: 32, marginBottom: 8 }} />
          <div>{t('pages.tariffs.emptyDesc')}</div>
        </div>
      ) : (
        <Table<Tariff>
          dataSource={tariffs}
          rowKey="id"
          size="small"
          pagination={false}
          scroll={{ x: 700 }}
          loading={loading}
          columns={[
            {
              title: t('pages.tariffs.name'),
              dataIndex: 'name',
              key: 'name',
              width: 160,
              render: (name: string) => <Tag color="green" style={{ margin: 0 }}>{name}</Tag>,
            },
            {
              title: t('pages.tariffs.trafficStrategy'),
              dataIndex: 'trafficStrategy',
              key: 'trafficStrategy',
              width: 140,
              render: (v: string) => <Tag color={v === 'sum' ? 'purple' : 'blue'} style={{ margin: 0 }}>{strategyLabel[v] ?? v}</Tag>,
            },
            {
              title: t('pages.tariffs.inboundStrategy'),
              dataIndex: 'inboundStrategy',
              key: 'inboundStrategy',
              width: 140,
              render: (v: string) => <Tag color={v === 'union' ? 'orange' : 'blue'} style={{ margin: 0 }}>{strategyLabel[v] ?? v}</Tag>,
            },
            {
              title: t('pages.tariffs.inUseByGroupsCol'),
              dataIndex: 'groupCount',
              key: 'groupCount',
              width: 120,
              render: (v: number) => v || 0,
            },
            {
              title: '',
              key: 'actions',
              width: 80,
              render: (_v, row) => (
                <Space size={4}>
                  <Button size="small" type="text" icon={<InfoCircleOutlined />}
                    onClick={async () => {
                      const msg = await HttpUtil.get<Tariff>(`/panel/api/clients/tariffs/${row.id}`, undefined, { silent: true });
                      setViewingTariff(msg?.success ? msg.obj ?? row : row);
                    }} />
                  <Button size="small" type="text" icon={<EditOutlined />}
                    onClick={async () => {
                      const msg = await HttpUtil.get<Tariff>(`/panel/api/clients/tariffs/${row.id}`, undefined, { silent: true });
                      setEditingTariff(msg?.success ? msg.obj ?? row : row);
                      setTariffModalOpen(true);
                    }} />
                  <Button size="small" type="text" danger icon={<DeleteOutlined />}
                    onClick={() => {
                      modal.confirm({
                        title: t('pages.tariffs.deleteConfirmTitle', { name: row.name }),
                        content: (row.groupCount || 0) > 0
                          ? `${t('pages.tariffs.inUseByGroups', { count: row.groupCount })}. ${t('pages.tariffs.cannotDeleteInUse')}`
                          : t('pages.tariffs.deleteConfirmContent'),
                        okText: t('delete'),
                        okType: 'danger',
                        okButtonProps: { disabled: (row.groupCount || 0) > 0 },
                        cancelText: t('cancel'),
                        onOk: () => tariffDeleteMut.mutate(row.id),
                      });
                    }} />
                </Space>
              ),
            },
          ]}
        />
      )}

      <TariffFormModal
        open={tariffModalOpen}
        editingTariff={editingTariff}
        profiles={profiles}
        inboundLabelById={inboundLabelById}
        saving={tariffCreateMut.isPending || tariffUpdateMut.isPending || tariffSetProfilesMut.isPending}
        onClose={() => setTariffModalOpen(false)}
        onSave={handleTariffSave}
      />

      <TariffFormModal
        open={viewingTariff !== null}
        editingTariff={viewingTariff}
        profiles={profiles}
        inboundLabelById={inboundLabelById}
        saving={false}
        readonly
        onClose={() => setViewingTariff(null)}
      />
    </>
  );
}
