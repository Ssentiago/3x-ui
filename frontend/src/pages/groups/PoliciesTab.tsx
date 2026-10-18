import { useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
} from 'antd';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';
import { keys } from '@/api/queryKeys';
import { formatInboundLabel } from '@/lib/inbounds/label';
import { PolicySchema, type Policy, type PolicyInput } from '@/schemas/policy';
import { InboundOptionsSchema, type InboundOption } from '@/schemas/client';

const PolicyListSchema = z
  .array(PolicySchema)
  .nullable()
  .transform((v) => v ?? []);
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

const MULTI_CLIENT_PROTOCOLS = new Set([
  'shadowsocks',
  'vless',
  'vmess',
  'trojan',
  'hysteria',
  'wireguard',
  'mtproto',
  'amneziawg',
  'tuic',
  'masque',
]);

async function fetchPolicies(): Promise<Policy[]> {
  const msg = await HttpUtil.get('/panel/api/clients/policies', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || '');
  return parseMsg(msg, PolicyListSchema, 'policies')?.obj ?? [];
}

async function fetchInboundOptions(): Promise<InboundOption[]> {
  const msg = await HttpUtil.get('/panel/api/inbounds/options', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || '');
  return parseMsg(msg, InboundOptionsSchema, 'inbounds/options')?.obj ?? [];
}

interface PolicyFormValues {
  name: string;
  trafficGB: number | null;
  expiryDays: number | null;
  limitIp: number | null;
  inboundIds: number[];
}

export default function PoliciesTab() {
  const { t } = useTranslation();
  const [messageApi, messageContextHolder] = message.useMessage();
  const [modal, modalContextHolder] = Modal.useModal();
  const queryClient = useQueryClient();
  const [editOpen, setEditOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Policy | null>(null);
  const [form] = Form.useForm<PolicyFormValues>();

  const policiesQuery = useQuery({
    queryKey: keys.clients.policies(),
    queryFn: fetchPolicies,
  });

  const inboundsQuery = useQuery({
    queryKey: keys.inbounds.options(),
    queryFn: fetchInboundOptions,
  });

  const saveMutation = useMutation({
    mutationFn: async ({ id, body }: { id?: number; body: PolicyInput }) => {
      const path = id
        ? `/panel/api/clients/policies/${id}/update`
        : '/panel/api/clients/policies/create';
      const msg = await HttpUtil.post(path, body, { ...JSON_HEADERS, silent: true });
      if (!msg.success) throw new Error(msg.msg || t('somethingWentWrong'));
      return msg;
    },
    onSuccess: () => {
      messageApi.success(t('saveSuccess'));
      queryClient.invalidateQueries({ queryKey: keys.clients.policies() });
      setEditOpen(false);
    },
    onError: (e: Error) => messageApi.error(e.message || t('somethingWentWrong')),
  });

  const deleteMutation = useMutation({
    mutationFn: async (id: number) => {
      const msg = await HttpUtil.post(
        `/panel/api/clients/policies/${id}/delete`,
        {},
        { ...JSON_HEADERS, silent: true },
      );
      if (!msg.success) throw new Error(msg.msg || t('somethingWentWrong'));
      return msg;
    },
    onSuccess: () => {
      messageApi.success(t('deleteSuccess'));
      queryClient.invalidateQueries({ queryKey: keys.clients.policies() });
    },
    onError: (e: Error) => messageApi.error(e.message || t('somethingWentWrong')),
  });

  const policies = useMemo(() => policiesQuery.data ?? [], [policiesQuery.data]);
  const allInbounds = useMemo(() => inboundsQuery.data ?? [], [inboundsQuery.data]);

  const inboundOptions = useMemo(
    () =>
      allInbounds
        .filter((ib) => MULTI_CLIENT_PROTOCOLS.has(ib.protocol || ''))
        .map((ib) => ({
          label: formatInboundLabel(ib.tag, ib.remark),
          value: ib.id,
        })),
    [allInbounds],
  );

  const inboundLabelById = useMemo(() => {
    const map = new Map<number, string>();
    for (const ib of allInbounds) {
      map.set(ib.id, formatInboundLabel(ib.tag, ib.remark));
    }
    return map;
  }, [allInbounds]);

  const openCreate = useCallback(() => {
    setEditTarget(null);
    form.resetFields();
    form.setFieldsValue({
      name: '',
      trafficGB: null,
      expiryDays: null,
      limitIp: null,
      inboundIds: [],
    });
    setEditOpen(true);
  }, [form]);

  const openEdit = useCallback(
    (p: Policy) => {
      setEditTarget(p);
      form.setFieldsValue({
        name: p.name,
        trafficGB: p.trafficGB ?? null,
        expiryDays: p.expiryDays ?? null,
        limitIp: p.limitIp ?? null,
        inboundIds: p.inboundIds ?? [],
      });
      setEditOpen(true);
    },
    [form],
  );

  const handleSave = useCallback(async () => {
    try {
      const values = await form.validateFields();
      const body: PolicyInput = {
        name: values.name.trim(),
        trafficGB: values.trafficGB ?? null,
        expiryDays: values.expiryDays ?? null,
        limitIp: values.limitIp ?? null,
        inboundIds: values.inboundIds ?? [],
      };
      await saveMutation.mutateAsync({ id: editTarget?.id, body });
    } catch {
      // validateFields rejects on validation error; mutation errors are handled by onError
    }
  }, [form, editTarget, saveMutation]);

  const handleDelete = useCallback(
    (p: Policy) => {
      modal.confirm({
        title: t('delete'),
        content: p.name,
        okText: t('delete'),
        okType: 'danger',
        cancelText: t('cancel'),
        onOk: () => deleteMutation.mutate(p.id),
      });
    },
    [modal, t, deleteMutation],
  );

  return (
    <>
      {messageContextHolder}
      {modalContextHolder}
      <Card
        size="small"
        hoverable
        title={
          <div className="card-toolbar">
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
              {t('pages.policies.addPolicy')}
            </Button>
          </div>
        }
      >
        <Table<Policy>
          dataSource={policies}
          rowKey="id"
          size="small"
          pagination={false}
          loading={policiesQuery.isLoading}
          columns={[
            {
              title: t('pages.policies.name'),
              dataIndex: 'name',
              render: (name: string) => <Tag color="purple">{name}</Tag>,
            },
            {
              title: t('pages.policies.trafficGB'),
              dataIndex: 'trafficGB',
              width: 120,
              render: (v: number | null | undefined) =>
                v == null ? <Tag>{t('pages.policies.unmanaged')}</Tag> : `${v} GB`,
            },
            {
              title: t('pages.policies.expiryDays'),
              dataIndex: 'expiryDays',
              width: 120,
              render: (v: number | null | undefined) =>
                v == null ? (
                  <Tag>{t('pages.policies.unmanaged')}</Tag>
                ) : (
                  `${v} ${t('pages.policies.days')}`
                ),
            },
            {
              title: t('pages.policies.limitIp'),
              dataIndex: 'limitIp',
              width: 100,
              render: (v: number | null | undefined) =>
                v == null ? <Tag>{t('pages.policies.unmanaged')}</Tag> : String(v),
            },
            {
              title: t('pages.policies.inboundIds'),
              dataIndex: 'inboundIds',
              render: (ids: number[] | null | undefined) =>
                !ids || ids.length === 0 ? (
                  <Tag>{t('pages.policies.unmanaged')}</Tag>
                ) : (
                  <Space size={2} wrap>
                    {ids.map((id) => (
                      <Tag key={id} style={{ margin: 0 }}>
                        {inboundLabelById.get(id) || `#${id}`}
                      </Tag>
                    ))}
                  </Space>
                ),
            },
            {
              title: t('pages.clients.actions'),
              key: 'actions',
              width: 90,
              render: (_v, row) => (
                <Space size={4}>
                  <Tooltip title={t('edit')}>
                    <Button
                      size="small"
                      type="text"
                      style={{ fontSize: 16 }}
                      icon={<EditOutlined />}
                      onClick={() => openEdit(row)}
                    />
                  </Tooltip>
                  <Tooltip title={t('delete')}>
                    <Button
                      size="small"
                      type="text"
                      danger
                      style={{ fontSize: 16 }}
                      icon={<DeleteOutlined />}
                      onClick={() => handleDelete(row)}
                    />
                  </Tooltip>
                </Space>
              ),
            },
          ]}
        />
      </Card>

      <Modal
        open={editOpen}
        title={editTarget ? t('pages.policies.editPolicy') : t('pages.policies.addPolicy')}
        okText={t('save')}
        cancelText={t('cancel')}
        confirmLoading={saveMutation.isPending}
        onCancel={() => setEditOpen(false)}
        onOk={handleSave}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item
            label={t('pages.policies.name')}
            name="name"
            rules={[{ required: true, message: t('required') }]}
          >
            <Input placeholder={t('pages.policies.namePlaceholder')} />
          </Form.Item>
          <Form.Item
            label={t('pages.policies.trafficGB')}
            name="trafficGB"
            extra={t('pages.policies.nullHint')}
          >
            <InputNumber min={0} style={{ width: '100%' }} placeholder="50" addonAfter="GB" />
          </Form.Item>
          <Form.Item
            label={t('pages.policies.expiryDays')}
            name="expiryDays"
            extra={t('pages.policies.nullHint')}
          >
            <InputNumber
              min={1}
              style={{ width: '100%' }}
              placeholder="30"
              addonAfter={t('pages.policies.days')}
            />
          </Form.Item>
          <Form.Item
            label={t('pages.policies.limitIp')}
            name="limitIp"
            extra={t('pages.policies.nullHint')}
          >
            <InputNumber min={0} style={{ width: '100%' }} placeholder="3" />
          </Form.Item>
          <Form.Item
            label={t('pages.clients.attachedInbounds')}
            name="inboundIds"
            extra={t('pages.policies.inboundsHint')}
          >
            <Select
              mode="multiple"
              options={inboundOptions}
              placeholder={t('pages.clients.selectInbound')}
              maxTagCount="responsive"
              listHeight={220}
              allowClear
              showSearch={{
                filterOption: (input, option) =>
                  ((option?.label as string) || '').toLowerCase().includes(input.toLowerCase()),
              }}
            />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
