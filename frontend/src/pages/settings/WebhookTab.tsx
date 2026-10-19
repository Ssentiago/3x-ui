import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Checkbox,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
} from 'antd';
import {
  ApiOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SendOutlined,
} from '@ant-design/icons';
import { HttpUtil } from '@/utils';
import { onNumber } from '@/utils/onNumber';
import { getMessage } from '@/utils/messageBus';
import { keys } from '@/api/queryKeys';
import type { AllSetting } from '@/models/setting';
import { SettingListItem } from '@/components/ui';
import SecretInput from './SecretInput';

interface WebhookEndpointRow {
  id: number;
  name: string;
  url: string;
  enabled: boolean;
  enabledEvents: string;
  allowPrivate: boolean;
  backupTransport: string;
  backupAttachFile: boolean;
  hasSecret: boolean;
}

interface WebhookTestResult {
  success: boolean;
  stage?: string;
  msg: string;
}

interface WebhookTabProps {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
}

const EVENT_OPTIONS = [
  'outbound.down',
  'outbound.up',
  'xray.crash',
  'node.down',
  'node.up',
  'cpu.high',
  'memory.high',
  'login.attempt',
  'db.backup.completed',
  'db.backup.failed',
];

interface EndpointForm {
  id: number;
  name: string;
  url: string;
  secret: string;
  clearSecret: boolean;
  enabled: boolean;
  enabledEvents: string[];
  allowPrivate: boolean;
  backupTransport: string;
  backupAttachFile: boolean;
}

function emptyForm(): EndpointForm {
  return {
    id: 0,
    name: '',
    url: '',
    secret: '',
    clearSecret: false,
    enabled: true,
    enabledEvents: ['login.attempt'],
    allowPrivate: false,
    backupTransport: 'raw',
    backupAttachFile: false,
  };
}

function toForm(row: WebhookEndpointRow): EndpointForm {
  return {
    id: row.id,
    name: row.name,
    url: row.url,
    secret: '',
    clearSecret: false,
    enabled: row.enabled,
    enabledEvents: row.enabledEvents
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean),
    allowPrivate: row.allowPrivate,
    backupTransport: row.backupTransport || 'raw',
    backupAttachFile: row.backupAttachFile,
  };
}

function toPayload(form: EndpointForm) {
  return {
    id: form.id,
    name: form.name,
    url: form.url,
    secret: form.secret,
    clearSecret: form.clearSecret,
    enabled: form.enabled,
    enabledEvents: form.enabledEvents.join(','),
    allowPrivate: form.allowPrivate,
    backupTransport: form.backupTransport,
    backupAttachFile: form.backupAttachFile,
  };
}

export default function WebhookTab({ allSetting, updateSetting }: WebhookTabProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState<EndpointForm | null>(null);
  const [testResult, setTestResult] = useState<WebhookTestResult | null>(null);
  const [testLoading, setTestLoading] = useState(false);

  const listQuery = useQuery({
    queryKey: keys.webhooks.list(),
    queryFn: async (): Promise<WebhookEndpointRow[]> => {
      const res = (await HttpUtil.get('/panel/api/webhooks', undefined, {
        silent: true,
      })) as { success?: boolean; obj?: WebhookEndpointRow[] };
      if (!res?.success) throw new Error(t('pages.settings.requestFailed'));
      return res.obj ?? [];
    },
  });
  const rows = listQuery.data ?? [];
  const loading = listQuery.isFetching;

  const invalidate = () => queryClient.invalidateQueries({ queryKey: keys.webhooks.list() });

  const saveMut = useMutation({
    mutationFn: async (form: EndpointForm) => {
      const payload = toPayload(form);
      const path = form.id ? `/panel/api/webhooks/update/${form.id}` : '/panel/api/webhooks/create';
      return (await HttpUtil.post(path, payload)) as { success?: boolean; msg?: string };
    },
    onSuccess: (res) => {
      if (!res?.success) {
        getMessage().error(res?.msg || t('pages.settings.requestFailed'));
        return;
      }
      getMessage().success(t('pages.settings.toasts.modifySettings'));
      setEditing(null);
      invalidate();
    },
    onError: () => getMessage().error(t('pages.settings.requestFailed')),
  });

  const deleteMut = useMutation({
    mutationFn: async (id: number) =>
      (await HttpUtil.post(`/panel/api/webhooks/delete/${id}`)) as { success?: boolean },
    onSuccess: (res) => {
      if (!res?.success) {
        getMessage().error(t('pages.settings.requestFailed'));
        return;
      }
      invalidate();
    },
    onError: () => getMessage().error(t('pages.settings.requestFailed')),
  });

  async function testEndpoint(form: EndpointForm) {
    setTestLoading(true);
    setTestResult(null);
    try {
      const res = (await HttpUtil.post('/panel/api/webhooks/test', toPayload(form))) as {
        success?: boolean;
        stage?: string;
        msg: string;
      };
      setTestResult({ success: !!res?.success, stage: res?.stage, msg: res?.msg });
    } catch (e: unknown) {
      setTestResult({
        success: false,
        msg: e instanceof Error ? e.message : t('pages.settings.requestFailed'),
      });
    } finally {
      setTestLoading(false);
    }
  }

  const columns = [
    {
      title: t('pages.settings.webhookName'),
      dataIndex: 'name',
    },
    {
      title: t('pages.settings.webhookUrl'),
      dataIndex: 'url',
      render: (v: string) => <code>{v}</code>,
    },
    {
      title: t('pages.settings.webhookEnabledEvents'),
      dataIndex: 'enabledEvents',
      render: (v: string) => v || '—',
    },
    {
      title: t('pages.settings.webhookEnabled'),
      dataIndex: 'enabled',
      render: (v: boolean) => (v ? '✓' : '—'),
    },
    {
      title: '',
      key: 'actions',
      render: (_: unknown, row: WebhookEndpointRow) => (
        <Space>
          <Button
            size="small"
            icon={<SendOutlined />}
            loading={testLoading}
            onClick={() => void testEndpoint(toForm(row))}
          />
          <Button size="small" icon={<EditOutlined />} onClick={() => setEditing(toForm(row))} />
          <Popconfirm
            title={t('pages.settings.webhookDeleteConfirm')}
            onConfirm={() => deleteMut.mutate(row.id)}
          >
            <Button size="small" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <>
      <SettingListItem
        paddings="small"
        title={t('pages.settings.webhookSourceName')}
        description={t('pages.settings.webhookSourceNameDesc')}
      >
        <Input
          value={allSetting.webhookSourceName}
          placeholder={t('pages.settings.webhookSourceNamePlaceholder')}
          onChange={(e) => updateSetting({ webhookSourceName: e.target.value })}
          style={{ maxWidth: 280 }}
        />
      </SettingListItem>

      <SettingListItem
        paddings="small"
        title={t('pages.settings.webhookThresholds')}
        description={t('pages.settings.webhookThresholdsDesc')}
      >
        <Space>
          <span>
            {t('pages.settings.eventCPUHigh')}:{' '}
            <InputNumber
              min={0}
              max={100}
              value={allSetting.webhookCpu}
              onChange={onNumber((v) => updateSetting({ webhookCpu: v }))}
              style={{ width: 80 }}
            />
          </span>
          <span>
            {t('pages.settings.eventMemoryHigh')}:{' '}
            <InputNumber
              min={0}
              max={100}
              value={allSetting.webhookMemory}
              onChange={onNumber((v) => updateSetting({ webhookMemory: v }))}
              style={{ width: 80 }}
            />
          </span>
        </Space>
      </SettingListItem>

      <SettingListItem
        paddings="small"
        title={t('pages.settings.webhookEndpoints')}
        description={t('pages.settings.webhookEndpointsDesc')}
      >
        <Button
          type="primary"
          icon={<PlusOutlined />}
          style={{ marginBottom: 12 }}
          onClick={() => setEditing(emptyForm())}
        >
          {t('pages.settings.webhookAdd')}
        </Button>
        <Table
          rowKey="id"
          size="small"
          loading={loading}
          columns={columns}
          dataSource={rows}
          pagination={false}
        />
      </SettingListItem>

      {testResult && (
        <Alert
          type={testResult.success ? 'success' : 'error'}
          title={
            testResult.success ? (
              t('pages.settings.' + testResult.msg)
            ) : (
              <span>
                <b>{testResult.stage}:</b> {t('pages.settings.' + testResult.msg)}
              </span>
            )
          }
          showIcon
          closable={{ onClose: () => setTestResult(null) }}
          style={{ marginTop: 12 }}
        />
      )}

      <Modal
        open={editing !== null}
        title={editing?.id ? t('pages.settings.webhookEdit') : t('pages.settings.webhookAdd')}
        onCancel={() => setEditing(null)}
        onOk={() => {
          if (editing) saveMut.mutate(editing);
        }}
        okText={t('save')}
        cancelText={t('cancel')}
        width={560}
      >
        {editing && (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <div>
              <div>{t('pages.settings.webhookName')}</div>
              <Input
                value={editing.name}
                onChange={(e) => setEditing({ ...editing, name: e.target.value })}
              />
            </div>
            <div>
              <div>{t('pages.settings.webhookUrl')}</div>
              <Input
                value={editing.url}
                placeholder="https://n8n.example.com/hook/3x-ui"
                onChange={(e) => setEditing({ ...editing, url: e.target.value })}
              />
            </div>
            <div>
              <div>
                {t('pages.settings.webhookSecret')}
                <span style={{ opacity: 0.65, marginLeft: 8, fontSize: 12 }}>
                  {t('pages.settings.webhookSecretConfigured')}
                </span>
              </div>
              <SecretInput
                value={editing.secret}
                configured={editing.id > 0 && !editing.clearSecret}
                clearArmed={editing.clearSecret}
                onChange={(v) => setEditing({ ...editing, secret: v })}
                onClearArmedChange={(armed) => setEditing({ ...editing, clearSecret: armed })}
                placeholder={t('pages.settings.webhookSecretPlaceholder')}
              />
            </div>
            <div>
              <div>{t('pages.settings.webhookEnabledEvents')}</div>
              <Checkbox.Group
                options={EVENT_OPTIONS}
                value={editing.enabledEvents}
                onChange={(vals) => setEditing({ ...editing, enabledEvents: vals as string[] })}
                style={{ display: 'flex', flexDirection: 'column', gap: 4 }}
              />
            </div>
            <div>
              <Checkbox
                checked={editing.allowPrivate}
                onChange={(e) => setEditing({ ...editing, allowPrivate: e.target.checked })}
              >
                {t('pages.settings.webhookAllowPrivate')}
              </Checkbox>
            </div>
            <div>
              <div>{t('pages.settings.webhookBackupTransport')}</div>
              <Select
                value={editing.backupTransport}
                onChange={(v) => setEditing({ ...editing, backupTransport: v })}
                options={[
                  { value: 'raw', label: t('pages.settings.webhookBackupRaw') },
                  {
                    value: 'json_base64',
                    label: t('pages.settings.webhookBackupJsonBase64'),
                  },
                ]}
                style={{ width: '100%' }}
              />
            </div>
            <div>
              <Checkbox
                checked={editing.backupAttachFile}
                onChange={(e) => setEditing({ ...editing, backupAttachFile: e.target.checked })}
              >
                {t('pages.settings.webhookBackupAttachFile')}
              </Checkbox>
            </div>
            <div>
              <Checkbox
                checked={editing.enabled}
                onChange={(e) => setEditing({ ...editing, enabled: e.target.checked })}
              >
                {t('pages.settings.webhookEnabled')}
              </Checkbox>
            </div>
            <Button
              icon={<ApiOutlined />}
              loading={testLoading}
              onClick={() => void testEndpoint(editing)}
            >
              {t('pages.settings.webhookTest')}
            </Button>
          </Space>
        )}
      </Modal>
    </>
  );
}
