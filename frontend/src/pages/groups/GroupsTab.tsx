import { lazy, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { z } from 'zod';
import {
  Button,
  Card,
  Col,
  Dropdown,
  Modal,
  Row,
  Space,
  Statistic,
  Table,
  Tag,
  Tooltip,
  message,
} from 'antd';
import type { MenuProps, TableColumnsType } from 'antd';
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  ClockCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  LinkOutlined,
  MoreOutlined,
  PieChartOutlined,
  PlusOutlined,
  RetweetOutlined,
  SafetyCertificateOutlined,
  TagsOutlined,
  TeamOutlined,
  UsergroupAddOutlined,
  UsergroupDeleteOutlined,
} from '@ant-design/icons';
import { useMutation, useQuery } from '@tanstack/react-query';
import { keys } from '@/api/queryKeys';
import { useClients } from '@/hooks/useClients';
import { HttpUtil, SizeFormatter } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';
import { LazyMount } from '@/components/utility';
import {
  type ClientRecord,
  type GroupSummary,
} from '@/schemas/client';
import { ClientRecordSchema } from '@/schemas/client';

const SubLinksModal = lazy(() => import('../clients/SubLinksModal'));
const ClientBulkAdjustModal = lazy(() => import('../clients/ClientBulkAdjustModal'));
const GroupAddClientsModal = lazy(() => import('./GroupAddClientsModal'));
const GroupRemoveClientsModal = lazy(() => import('./GroupRemoveClientsModal'));
import GroupFormModal from './GroupFormModal';

const ClientRecordListSchema = z.array(ClientRecordSchema).nullable().transform((v) => v ?? []);

const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

async function fetchEmailsForGroup(name: string): Promise<string[]> {
  const msg = await HttpUtil.get<string[]>(
    `/panel/api/clients/groups/${encodeURIComponent(name)}/emails`,
    undefined,
    { silent: true },
  );
  if (!msg?.success || !Array.isArray(msg.obj)) return [];
  return msg.obj;
}

interface GroupsTabProps {
  groups: GroupSummary[];
  loading: boolean;
  tariffs: { id: number; name: string }[];
  invalidate: () => void;
}

export default function GroupsTab({ groups, loading, tariffs, invalidate }: GroupsTabProps) {
  const { t } = useTranslation();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();

  const { subSettings, bulkAdjust, bulkAddToGroup, bulkRemoveFromGroup, bulkDelete } = useClients({ list: false });

  const { data: allClients = [] } = useQuery<ClientRecord[]>({
    queryKey: keys.clients.all(),
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/clients/list', undefined, { silent: true });
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to load clients');
      const validated = parseMsg(msg, ClientRecordListSchema, 'clients/list');
      return validated.obj ?? [];
    },
    staleTime: 30_000,
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [createName, setCreateName] = useState('');
  const [createTariffId, setCreateTariffId] = useState<number | null>(null);

  const [renameOpen, setRenameOpen] = useState(false);
  const [renameTarget, setRenameTarget] = useState<GroupSummary | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [renameTariffId, setRenameTariffId] = useState<number | null>(null);

  const [subLinksOpen, setSubLinksOpen] = useState(false);
  const [adjustOpen, setAdjustOpen] = useState(false);
  const [addClientsOpen, setAddClientsOpen] = useState(false);
  const [removeClientsOpen, setRemoveClientsOpen] = useState(false);
  const [groupEmails, setGroupEmails] = useState<string[]>([]);
  const [groupForAction, setGroupForAction] = useState<GroupSummary | null>(null);

  const createMut = useMutation({
    mutationFn: (body: { name: string; tariffId?: number | null }) =>
      HttpUtil.post('/panel/api/clients/groups/create', body, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const renameMut = useMutation({
    mutationFn: (body: { oldName: string; newName: string; tariffId?: number | null }) =>
      HttpUtil.post('/panel/api/clients/groups/rename', body, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const deleteMut = useMutation({
    mutationFn: (body: { name: string }) =>
      HttpUtil.post('/panel/api/clients/groups/delete', body, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const groupResetMut = useMutation({
    mutationFn: (body: { name: string }) =>
      HttpUtil.post('/panel/api/clients/groups/resetTraffic', body, JSON_HEADERS),
    onSuccess: (msg) => { if (msg?.success) invalidate(); },
  });

  const totalGroups = groups.length;
  const totalClients = useMemo(
    () => groups.reduce((acc, g) => acc + (g.clientCount || 0), 0),
    [groups],
  );
  const totalTraffic = useMemo(
    () => groups.reduce((acc, g) => acc + (g.trafficUsed || 0), 0),
    [groups],
  );
  const totalUpload = useMemo(
    () => groups.reduce((acc, g) => acc + (g.up || 0), 0),
    [groups],
  );
  const totalDownload = useMemo(
    () => groups.reduce((acc, g) => acc + (g.down || 0), 0),
    [groups],
  );

  function openCreate() {
    setCreateName('');
    setCreateTariffId(null);
    setCreateOpen(true);
  }

  async function confirmCreate() {
    const name = createName.trim();
    if (!name) return;
    if (groups.some((g) => g.name.toLowerCase() === name.toLowerCase())) {
      messageApi.error(t('pages.groups.renameCollision', { name }));
      return;
    }
    const msg = await createMut.mutateAsync({ name, tariffId: createTariffId });
    if (msg?.success) {
      messageApi.success(t('pages.groups.createSuccess', { name }));
      setCreateOpen(false);
      setCreateName('');
      setCreateTariffId(null);
    }
  }

  function openRename(g: GroupSummary) {
    setRenameTarget(g);
    setRenameValue(g.name);
    setRenameTariffId(g.tariffId ?? null);
    setRenameOpen(true);
  }

  async function confirmRename() {
    if (!renameTarget) return;
    const next = renameValue.trim();
    const tariffChanged = renameTariffId !== (renameTarget.tariffId ?? null);
    if (!next && !tariffChanged) {
      setRenameOpen(false);
      return;
    }
    const name = next || renameTarget.name;
    if (next && groups.some((g) => g.name.toLowerCase() === next.toLowerCase() && g.name !== renameTarget.name)) {
      messageApi.error(t('pages.groups.renameCollision', { name: next }));
      return;
    }
    const msg = await renameMut.mutateAsync({
      oldName: renameTarget.name,
      newName: name,
      tariffId: renameTariffId,
    });
    if (msg?.success) {
      messageApi.success(t('pages.groups.editSuccess'));
      setRenameOpen(false);
    }
  }

  function onDelete(g: GroupSummary) {
    modal.confirm({
      title: t('pages.groups.deleteConfirmTitle', { name: g.name }),
      content: t('pages.groups.deleteConfirmContent', { count: g.clientCount }),
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await deleteMut.mutateAsync({ name: g.name });
        if (msg?.success) {
          messageApi.success(t('pages.groups.deleteSuccess'));
        }
      },
    });
  }

  async function openSubLinksFor(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    const emails = await fetchEmailsForGroup(g.name);
    if (emails.length === 0) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    setGroupForAction(g);
    setGroupEmails(emails);
    setSubLinksOpen(true);
  }

  async function openAdjustFor(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    const emails = await fetchEmailsForGroup(g.name);
    if (emails.length === 0) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    setGroupForAction(g);
    setGroupEmails(emails);
    setAdjustOpen(true);
  }

  function openAddClientsFor(g: GroupSummary) {
    setGroupForAction(g);
    setAddClientsOpen(true);
  }

  function openRemoveClientsFor(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    setGroupForAction(g);
    setRemoveClientsOpen(true);
  }

  function onDeleteClients(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    modal.confirm({
      title: t('pages.groups.deleteClientsConfirmTitle', { name: g.name }),
      content: t('pages.groups.deleteClientsConfirmContent', { count: g.clientCount }),
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const emails = await fetchEmailsForGroup(g.name);
        if (emails.length === 0) return;
        const msg = await bulkDelete(emails);
        if (msg?.success) {
          const ok = msg.obj?.deleted ?? 0;
          const skipped = msg.obj?.skipped ?? [];
          const failed = skipped.length;
          if (failed === 0) {
            messageApi.success(t('pages.groups.deleteClientsSuccess', { count: ok }));
          } else {
            const firstError = skipped[0]?.reason ?? msg?.msg ?? '';
            messageApi.warning(firstError
              ? `${t('pages.groups.deleteClientsMixed', { ok, failed })} — ${firstError}`
              : t('pages.groups.deleteClientsMixed', { ok, failed }));
          }
        }
      },
    });
  }

  function onResetTraffic(g: GroupSummary) {
    if (!g.clientCount) {
      messageApi.info(t('pages.groups.emptyForAction'));
      return;
    }
    modal.confirm({
      title: t('pages.groups.resetConfirmTitle', { name: g.name }),
      content: t('pages.groups.resetConfirmContent'),
      okText: t('reset'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await groupResetMut.mutateAsync({ name: g.name });
        if (msg?.success) {
          messageApi.success(t('pages.groups.resetSuccess', { name: g.name }));
        }
      },
    });
  }

  function onResetTariff(g: GroupSummary) {
    modal.confirm({
      title: t('pages.groups.removeTariffConfirm', { name: g.name }),
      content: t('pages.groups.removeTariffContent'),
      okText: t('remove'),
      cancelText: t('cancel'),
      onOk: async () => {
        const msg = await HttpUtil.post('/panel/api/clients/groups/resetTariff', { name: g.name }, JSON_HEADERS);
        if (msg?.success) {
          messageApi.success(t('pages.groups.removeTariffSuccess'));
          invalidate();
        }
      },
    });
  }

  function rowActions(row: GroupSummary): MenuProps['items'] {
    return [
      {
        key: 'subLinks',
        icon: <LinkOutlined />,
        label: t('pages.clients.subLinksSelected', { count: row.clientCount || 0 }),
        disabled: !row.clientCount,
        onClick: () => openSubLinksFor(row),
      },
      {
        key: 'adjust',
        icon: <ClockCircleOutlined />,
        label: t('pages.clients.adjustSelected', { count: row.clientCount || 0 }),
        disabled: !row.clientCount,
        onClick: () => openAdjustFor(row),
      },
      {
        key: 'reset',
        icon: <RetweetOutlined />,
        label: t('pages.groups.resetTraffic'),
        disabled: !row.clientCount,
        onClick: () => onResetTraffic(row),
      },
      {
        key: 'addClients',
        icon: <UsergroupAddOutlined />,
        label: t('pages.groups.addToGroup'),
        onClick: () => openAddClientsFor(row),
      },
      {
        key: 'rename',
        icon: <EditOutlined />,
        label: t('pages.groups.edit'),
        onClick: () => openRename(row),
      },
      { type: 'divider' },
      {
        key: 'removeClients',
        icon: <UsergroupDeleteOutlined />,
        label: t('pages.groups.removeFromGroup'),
        danger: true,
        disabled: !row.clientCount,
        onClick: () => openRemoveClientsFor(row),
      },
      ...(row.tariffId ? [{
        key: 'resetTariff',
        icon: <SafetyCertificateOutlined />,
        label: t('pages.groups.removeTariff'),
        onClick: () => onResetTariff(row),
      }] : []),
      {
        key: 'deleteClients',
        icon: <DeleteOutlined />,
        label: t('pages.groups.deleteClients'),
        danger: true,
        disabled: !row.clientCount,
        onClick: () => onDeleteClients(row),
      },
      {
        key: 'delete',
        icon: <DeleteOutlined />,
        label: t('pages.groups.deleteGroupOnly'),
        danger: true,
        onClick: () => onDelete(row),
      },
    ];
  }

  const columns: TableColumnsType<GroupSummary> = [
    {
      title: t('pages.clients.actions'),
      key: 'actions',
      width: 90,
      render: (_v, row) => (
        <Space size={4}>
          <Dropdown trigger={['click']} menu={{ items: rowActions(row) }}>
            <Button aria-label={t('more')} size="small" type="text" style={{ fontSize: 16 }} icon={<MoreOutlined />} />
          </Dropdown>
          <Tooltip title={t('pages.groups.edit')}>
            <Button aria-label={t('pages.groups.edit')} size="small" type="text" style={{ fontSize: 16 }} icon={<EditOutlined />} onClick={() => openRename(row)} />
          </Tooltip>
        </Space>
      ),
    },
    {
      title: t('pages.groups.name'),
      dataIndex: 'name',
      key: 'name',
      render: (name: string) => <Tag color="geekblue" style={{ margin: 0, fontSize: 13 }}>{name}</Tag>,
    },
    {
      title: t('pages.groups.tariffColumn'),
      dataIndex: 'tariffName',
      key: 'tariffName',
      width: 140,
      render: (name: string) => name ? <Tag color="green">{name}</Tag> : <span style={{ color: '#999' }}>{t('pages.groups.noTariff')}</span>,
    },
    {
      title: t('pages.groups.clientCount'),
      dataIndex: 'clientCount',
      key: 'clientCount',
      width: 180,
      render: (count: number) => <span>{count || 0}</span>,
    },
    {
      title: t('pages.groups.upload'),
      dataIndex: 'up',
      key: 'up',
      width: 140,
      render: (bytes: number) => <span>{SizeFormatter.sizeFormat(bytes || 0)}</span>,
    },
    {
      title: t('pages.groups.download'),
      dataIndex: 'down',
      key: 'down',
      width: 140,
      render: (bytes: number) => <span>{SizeFormatter.sizeFormat(bytes || 0)}</span>,
    },
    {
      title: t('pages.groups.trafficUsed'),
      dataIndex: 'trafficUsed',
      key: 'trafficUsed',
      width: 160,
      render: (bytes: number) => <span>{SizeFormatter.sizeFormat(bytes || 0)}</span>,
    },
  ];

  return (
    <>
      {messageContextHolder}
      {modalContextHolder}
      <Card size="small" hoverable className="summary-card" style={{ marginBottom: 16 }}>
        <Row gutter={[16, 16]}>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title={t('pages.groups.totalGroups')}
              value={String(totalGroups)}
              prefix={<TagsOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title={t('pages.groups.totalGroupedClients')}
              value={String(totalClients)}
              prefix={<TeamOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title={t('pages.groups.totalUpDown')}
              value={0}
              formatter={() => (
                <span>
                  <ArrowUpOutlined /> {SizeFormatter.sizeFormat(totalUpload)}
                  {' / '}
                  <ArrowDownOutlined /> {SizeFormatter.sizeFormat(totalDownload)}
                </span>
              )}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title={t('pages.groups.totalTraffic')}
              value={SizeFormatter.sizeFormat(totalTraffic)}
              prefix={<PieChartOutlined />}
            />
          </Col>
        </Row>
      </Card>
      <div className="card-toolbar" style={{ marginBottom: 12 }}>
        <Button aria-label={t('pages.groups.addGroup')} type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          {t('pages.groups.addGroup')}
        </Button>
      </div>
      <Table<GroupSummary>
        dataSource={groups}
        columns={columns}
        rowKey="name"
        size="small"
        pagination={false}
        loading={loading}
        locale={{
          emptyText: (
            <div className="card-empty">
              <div style={{ fontSize: 32, marginBottom: 8 }}>{'{ }'}</div>
              <div>{t('noData')}</div>
            </div>
          ),
        }}
      />

      <GroupFormModal
        open={createOpen}
        title={t('pages.groups.addGroup')}
        saving={createMut.isPending}
        nameValue={createName}
        tariffIdValue={createTariffId}
        onNameChange={setCreateName}
        onTariffIdChange={setCreateTariffId}
        onOk={confirmCreate}
        onCancel={() => setCreateOpen(false)}
        tariffs={tariffs}
      />

      <GroupFormModal
        open={renameOpen}
        title={renameTarget ? t('pages.groups.editTitle', { name: renameTarget.name }) : ''}
        saving={renameMut.isPending}
        nameValue={renameValue}
        tariffIdValue={renameTariffId}
        onNameChange={setRenameValue}
        onTariffIdChange={setRenameTariffId}
        onOk={confirmRename}
        onCancel={() => setRenameOpen(false)}
        tariffs={tariffs}
      />

      <LazyMount when={subLinksOpen}>
        <SubLinksModal
          open={subLinksOpen}
          emails={groupEmails}
          clients={allClients}
          subSettings={subSettings}
          onOpenChange={setSubLinksOpen}
        />
      </LazyMount>

      <LazyMount when={adjustOpen}>
        <ClientBulkAdjustModal
          open={adjustOpen}
          count={groupEmails.length}
          onOpenChange={setAdjustOpen}
          onSubmit={async (addDays, addBytes) => {
            const msg = await bulkAdjust(groupEmails, addDays, addBytes);
            if (msg?.success) {
              const obj = msg.obj ?? { adjusted: 0 };
              messageApi.success(
                t('pages.groups.adjustSuccess', {
                  count: obj.adjusted ?? 0,
                  name: groupForAction?.name ?? '',
                }),
              );
              return obj;
            }
            return null;
          }}
        />
      </LazyMount>

      <LazyMount when={addClientsOpen}>
        <GroupAddClientsModal
          open={addClientsOpen}
          groupName={groupForAction?.name ?? null}
          candidates={allClients.filter((c) => c.group !== groupForAction?.name)}
          onClose={() => setAddClientsOpen(false)}
          onSubmit={async (emails) => {
            const msg = await bulkAddToGroup(emails, groupForAction?.name ?? '');
            if (msg?.success) {
              return (msg.obj as { affected?: number } | undefined) ?? { affected: 0 };
            }
            return null;
          }}
        />
      </LazyMount>

      <LazyMount when={removeClientsOpen}>
        <GroupRemoveClientsModal
          open={removeClientsOpen}
          groupName={groupForAction?.name ?? null}
          members={allClients.filter((c) => c.group === groupForAction?.name)}
          onClose={() => setRemoveClientsOpen(false)}
          onSubmit={async (emails) => {
            const msg = await bulkRemoveFromGroup(emails);
            if (msg?.success) {
              return (msg.obj as { affected?: number } | undefined) ?? { affected: 0 };
            }
            return null;
          }}
        />
      </LazyMount>
    </>
  );
}
