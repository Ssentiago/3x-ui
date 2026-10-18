import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Divider,
  message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from 'antd';
import { SafetyCertificateOutlined } from '@ant-design/icons';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { HttpUtil } from '@/utils';
import { parseMsg } from '@/utils/zodValidate';
import { keys } from '@/api/queryKeys';
import { formatInboundLabel } from '@/lib/inbounds/label';
import {
  ApplyPreviewSchema,
  ApplyResultSchema,
  PolicySchema,
  type ApplyPreview,
  type ApplyResult,
  type Policy,
} from '@/schemas/policy';
import { InboundOptionsSchema, type InboundOption } from '@/schemas/client';

const PolicyListSchema = z
  .array(PolicySchema)
  .nullable()
  .transform((v) => v ?? []);
const StringListSchema = z
  .array(z.string())
  .nullable()
  .transform((v) => v ?? []);
const JSON_HEADERS = { headers: { 'Content-Type': 'application/json' } } as const;

interface GroupDetailModalProps {
  open: boolean;
  groupName: string;
  onClose: () => void;
}

interface ClientPreviewRow {
  email: string;
  totalGB?: string;
  expiryTime?: string;
  limitIP?: string;
  inbounds?: string;
}

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

async function fetchExceptions(group: string): Promise<string[]> {
  const msg = await HttpUtil.get(
    `/panel/api/clients/groups/${encodeURIComponent(group)}/exceptions`,
    undefined,
    { silent: true },
  );
  if (!msg?.success) throw new Error(msg?.msg || '');
  return parseMsg(msg, StringListSchema, 'exceptions')?.obj ?? [];
}

async function fetchEmails(group: string): Promise<string[]> {
  const msg = await HttpUtil.get(
    `/panel/api/clients/groups/${encodeURIComponent(group)}/emails`,
    undefined,
    { silent: true },
  );
  if (!msg?.success) throw new Error(msg?.msg || '');
  return (msg.obj as string[]) ?? [];
}

async function previewApply(group: string, policyId: number): Promise<ApplyPreview> {
  const msg = await HttpUtil.post(
    '/panel/api/clients/policies/preview',
    { group, policyId },
    JSON_HEADERS,
  );
  if (!msg?.success) throw new Error(msg?.msg || '');
  const parsed = parseMsg(msg, ApplyPreviewSchema, 'preview');
  return parsed.obj ?? { totalClients: 0, skipped: 0, changed: 0, fieldChanges: {}, changes: [] };
}

async function applyPolicy(group: string, policyId: number): Promise<ApplyResult> {
  const msg = await HttpUtil.post(
    '/panel/api/clients/policies/apply',
    { group, policyId },
    JSON_HEADERS,
  );
  if (!msg?.success) throw new Error(msg?.msg || '');
  const parsed = parseMsg(msg, ApplyResultSchema, 'apply');
  return parsed.obj ?? { totalClients: 0, skipped: 0, changed: 0, fieldChanges: {} };
}

export default function GroupDetailModal({ open, groupName, onClose }: GroupDetailModalProps) {
  const { t } = useTranslation();
  const [messageApi, messageContextHolder] = message.useMessage();
  const [modal, modalContextHolder] = Modal.useModal();
  const queryClient = useQueryClient();
  const [selectedPolicyId, setSelectedPolicyId] = useState<number | null>(null);
  const [preview, setPreview] = useState<ApplyPreview | null>(null);

  const policiesQuery = useQuery({
    queryKey: keys.clients.policies(),
    queryFn: fetchPolicies,
    enabled: open,
  });

  const inboundsQuery = useQuery({
    queryKey: keys.inbounds.options(),
    queryFn: fetchInboundOptions,
    enabled: open,
  });

  const exceptionsQuery = useQuery({
    queryKey: keys.clients.groupExceptions(groupName),
    queryFn: () => fetchExceptions(groupName),
    enabled: open && !!groupName,
  });

  const emailsQuery = useQuery({
    queryKey: keys.clients.groupEmails(groupName),
    queryFn: () => fetchEmails(groupName),
    enabled: open && !!groupName,
  });

  const previewMutation = useMutation({
    mutationFn: (policyId: number) => previewApply(groupName, policyId),
    onSuccess: setPreview,
    onError: (e: Error) => messageApi.error(e.message || t('somethingWentWrong')),
  });

  const applyMutation = useMutation({
    mutationFn: (policyId: number) => applyPolicy(groupName, policyId),
    onSuccess: (result) => {
      messageApi.success(
        t('pages.groups.applySuccess', {
          changed: result.changed,
          total: result.totalClients,
        }),
      );
      setPreview(null);
      queryClient.invalidateQueries({ queryKey: keys.clients.all() });
      queryClient.invalidateQueries({ queryKey: keys.clients.groupEmails(groupName) });
    },
    onError: (e: Error) => messageApi.error(e.message || t('somethingWentWrong')),
  });

  const setExceptionsMutation = useMutation({
    mutationFn: (emails: string[]) =>
      HttpUtil.post(
        `/panel/api/clients/groups/${encodeURIComponent(groupName)}/exceptions/set`,
        { emails },
        JSON_HEADERS,
      ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: keys.clients.groupExceptions(groupName) });
    },
    onError: (e: Error) => messageApi.error(e.message || t('somethingWentWrong')),
  });

  const exceptions = useMemo(() => exceptionsQuery.data ?? [], [exceptionsQuery.data]);
  const exceptionSet = useMemo(() => new Set(exceptions), [exceptions]);
  const emails = useMemo(() => emailsQuery.data ?? [], [emailsQuery.data]);
  const policies = useMemo(() => policiesQuery.data ?? [], [policiesQuery.data]);

  const inboundLabelById = useMemo(() => {
    const map = new Map<number, string>();
    for (const ib of inboundsQuery.data ?? []) {
      map.set(ib.id, formatInboundLabel(ib.tag, ib.remark));
    }
    return map;
  }, [inboundsQuery.data]);

  const clientRows = useMemo(
    () => emails.map((email) => ({ email, excluded: exceptionSet.has(email) })),
    [emails, exceptionSet],
  );

  const previewRows = useMemo(() => {
    if (!preview) return [];
    const map = new Map<string, ClientPreviewRow>();
    for (const c of preview.changes ?? []) {
      const row = map.get(c.email) ?? { email: c.email };
      const val = `${c.oldVal} → ${c.newVal}`;
      if (c.field === 'totalGB') row.totalGB = val;
      else if (c.field === 'expiryTime') row.expiryTime = val;
      else if (c.field === 'limitIP') row.limitIP = val;
      else if (c.field === 'inbounds') row.inbounds = val;
      map.set(c.email, row);
    }
    return Array.from(map.values());
  }, [preview]);

  const handleToggleException = useCallback(
    (email: string, excluded: boolean) => {
      const next = excluded ? exceptions.filter((e) => e !== email) : [...exceptions, email];
      setExceptionsMutation.mutate(next);
    },
    [exceptions, setExceptionsMutation],
  );

  useEffect(() => {
    if (selectedPolicyId == null) {
      setPreview(null);
      return;
    }
    previewMutation.mutate(selectedPolicyId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedPolicyId]);

  const handleApply = useCallback(() => {
    if (selectedPolicyId == null) return;
    modal.confirm({
      title: t('pages.groups.confirmApply'),
      content: t('pages.groups.confirmApplyDesc', {
        count: preview?.changed ?? 0,
        group: groupName,
      }),
      okText: t('apply'),
      cancelText: t('cancel'),
      onOk: () => applyMutation.mutate(selectedPolicyId),
    });
  }, [selectedPolicyId, preview, groupName, modal, t, applyMutation]);

  const policyOptions = useMemo(
    () => policies.map((p) => ({ value: p.id, label: p.name })),
    [policies],
  );

  const selectedPolicy = useMemo(
    () => policies.find((p) => p.id === selectedPolicyId) ?? null,
    [policies, selectedPolicyId],
  );

  return (
    <>
      {messageContextHolder}
      {modalContextHolder}
      <Modal
        open={open}
        title={t('pages.groups.groupDetail', { name: groupName })}
        width={860}
        onCancel={onClose}
        footer={null}
      >
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <div>
            <Typography.Title level={5}>
              {t('pages.groups.clientsTitle')}{' '}
              <Typography.Text type="secondary">({emails.length})</Typography.Text>
            </Typography.Title>
            <Table
              rowKey="email"
              size="small"
              dataSource={clientRows}
              pagination={clientRows.length > 20 ? { pageSize: 20 } : false}
              columns={[
                {
                  title: t('pages.clients.email'),
                  dataIndex: 'email',
                  render: (email: string) => email,
                },
                {
                  title: t('status'),
                  dataIndex: 'excluded',
                  width: 130,
                  render: (excluded: boolean, row) => (
                    <Tag
                      color={excluded ? 'orange' : 'green'}
                      style={{ cursor: 'pointer', userSelect: 'none' }}
                      onClick={() => handleToggleException(row.email, excluded)}
                    >
                      {excluded ? t('pages.groups.exception') : t('pages.groups.included')}
                    </Tag>
                  ),
                },
              ]}
            />
          </div>

          <Divider style={{ margin: '8px 0' }} />

          <div>
            <Typography.Title level={5}>{t('pages.groups.applyPolicy')}</Typography.Title>
            <Space direction="vertical" size="small" style={{ width: '100%' }}>
              <Space>
                <Select
                  style={{ minWidth: 260 }}
                  placeholder={t('pages.groups.selectPolicy')}
                  options={policyOptions}
                  value={selectedPolicyId}
                  onChange={(v: number) => setSelectedPolicyId(v)}
                  allowClear
                />
                <Button
                  type="primary"
                  icon={<SafetyCertificateOutlined />}
                  disabled={selectedPolicyId == null}
                  loading={applyMutation.isPending}
                  onClick={handleApply}
                >
                  {t('pages.groups.apply')}
                </Button>
              </Space>
              {selectedPolicy && (
                <div
                  style={{
                    padding: '8px 12px',
                    borderRadius: 6,
                    background: 'var(--ant-color-fill-quaternary)',
                  }}
                >
                  <Space size="large" wrap>
                    <Typography.Text type="secondary">
                      {t('pages.policies.trafficGB')}:{' '}
                      {selectedPolicy.trafficGB != null
                        ? `${selectedPolicy.trafficGB} GB`
                        : t('pages.policies.unmanaged')}
                    </Typography.Text>
                    <Typography.Text type="secondary">
                      {t('pages.policies.expiryDays')}:{' '}
                      {selectedPolicy.expiryDays != null
                        ? `${selectedPolicy.expiryDays} ${t('pages.policies.days')}`
                        : t('pages.policies.unmanaged')}
                    </Typography.Text>
                    <Typography.Text type="secondary">
                      {t('pages.policies.limitIp')}:{' '}
                      {selectedPolicy.limitIp != null
                        ? String(selectedPolicy.limitIp)
                        : t('pages.policies.unmanaged')}
                    </Typography.Text>
                    <Typography.Text type="secondary">
                      {t('pages.policies.inboundIds')}:{' '}
                      {selectedPolicy.inboundIds && selectedPolicy.inboundIds.length > 0
                        ? selectedPolicy.inboundIds
                            .map((id) => inboundLabelById.get(id) || `#${id}`)
                            .join(', ')
                        : t('pages.policies.unmanaged')}
                    </Typography.Text>
                  </Space>
                </div>
              )}
              {preview != null && (
                <Alert
                  type={preview.changed > 0 ? 'info' : 'success'}
                  showIcon
                  message={
                    preview.changed > 0
                      ? t('pages.groups.previewSummary', {
                          changed: preview.changed,
                          total: preview.totalClients,
                          skipped: preview.skipped,
                        })
                      : t('pages.groups.previewNoChanges')
                  }
                  description={
                    previewRows.length > 0 ? (
                      <Table<ClientPreviewRow>
                        rowKey="email"
                        size="small"
                        dataSource={previewRows}
                        pagination={previewRows.length > 10 ? { pageSize: 10 } : false}
                        columns={[
                          {
                            title: t('pages.clients.email'),
                            dataIndex: 'email',
                            width: 180,
                            ellipsis: true,
                          },
                          {
                            title: t('pages.policies.trafficGB'),
                            dataIndex: 'totalGB',
                            render: (v) => v ?? '—',
                          },
                          {
                            title: t('pages.policies.expiryDays'),
                            dataIndex: 'expiryTime',
                            render: (v) => v ?? '—',
                          },
                          {
                            title: t('pages.policies.limitIp'),
                            dataIndex: 'limitIP',
                            render: (v) => v ?? '—',
                          },
                          {
                            title: t('pages.policies.inboundIds'),
                            dataIndex: 'inbounds',
                            render: (v) => v ?? '—',
                          },
                        ]}
                      />
                    ) : null
                  }
                />
              )}
            </Space>
          </div>
        </Space>
      </Modal>
    </>
  );
}
