import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  Button,
  Card,
  Input,
  List,
  Modal,
  Popover,
  Select,
  Space,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  DeleteOutlined,
  InfoCircleOutlined,
  PlusOutlined,
  SearchOutlined,
} from '@ant-design/icons';
import { FormProvider, useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { FormField } from '@/components/form/rhf';
import { HttpUtil } from '@/utils';
import { TariffFormSchema, type Tariff, type TariffFormValues } from '@/schemas/tariff';
import type { Profile } from '@/schemas/profile';
import { TrafficResolutionStrategy } from '@/lib/tariff/strategies';
import { bytesPerGB } from '@/lib/clients/units';

interface ChainItem {
  profileId: number;
  name: string;
  traffic: number | null;
  expiryDays: number | null;
  limitIp: number | null;
  inboundIds: number[];
}

interface ResolvedPreview {
  trafficBytes: number;
  expiryDays: number;
  limitIp: number;
  inboundIds: number[];
  trafficSource: string;
  expirySource: string;
  ipSource: string;
  inboundSource: string;
}

interface TariffFormModalProps {
  open: boolean;
  editingTariff: Tariff | null;
  profiles: Profile[];
  inboundLabelById: Map<number, string>;
  saving: boolean;
  readonly?: boolean;
  onClose: () => void;
  onSave?: (
    values: TariffFormValues & { tariffId?: number },
    chain: { id: number; position: number }[],
  ) => Promise<void>;
}

export default function TariffFormModal({
  open,
  editingTariff,
  profiles,
  inboundLabelById,
  saving,
  readonly,
  onClose,
  onSave,
}: TariffFormModalProps) {
  const { t } = useTranslation();

  const methods = useForm<TariffFormValues>({
    resolver: zodResolver(TariffFormSchema),
    defaultValues: { name: '', trafficStrategy: TrafficResolutionStrategy.Overwrite, inboundStrategy: TrafficResolutionStrategy.Overwrite },
  });

  const [chain, setChain] = useState<ChainItem[]>([]);
  const [trafficStrategy, setTrafficStrategy] = useState<TrafficResolutionStrategy>(TrafficResolutionStrategy.Overwrite);
  const [inboundStrategy, setInboundStrategy] = useState<TrafficResolutionStrategy>(TrafficResolutionStrategy.Overwrite);
  const [addPopoverOpen, setAddPopoverOpen] = useState(false);
  const [profileSearch, setProfileSearch] = useState('');

  const profileById = useMemo(() => {
    const map = new Map<number, Profile>();
    for (const p of profiles) map.set(p.id, p);
    return map;
  }, [profiles]);

  const { data: resolved = { trafficBytes: 0, expiryDays: 0, limitIp: 0, inboundIds: [] as number[], trafficSource: '', expirySource: '', ipSource: '', inboundSource: '' } } = useQuery({
    queryKey: ['tariffChainPreview', chain, trafficStrategy, inboundStrategy],
    queryFn: async () => {
      const msg = await HttpUtil.post('/panel/api/clients/tariffs/preview', {
        profiles: chain,
        trafficStrategy,
        inboundStrategy,
      }, { headers: { 'Content-Type': 'application/json' } });
      return msg?.obj as ResolvedPreview;
    },
    enabled: chain.length > 0,
    placeholderData: { trafficBytes: 0, expiryDays: 0, limitIp: 0, inboundIds: [], trafficSource: '', expirySource: '', ipSource: '', inboundSource: '' } as ResolvedPreview,
  });

  const resetForm = useCallback(() => {
    methods.reset({ name: '', trafficStrategy: TrafficResolutionStrategy.Overwrite, inboundStrategy: TrafficResolutionStrategy.Overwrite });
    setChain([]);
    setTrafficStrategy(TrafficResolutionStrategy.Overwrite);
    setInboundStrategy(TrafficResolutionStrategy.Overwrite);
  }, [methods]);

  const initFromTariff = useCallback((row: Tariff) => {
    methods.reset({
      name: row.name,
      trafficStrategy: row.trafficStrategy,
      inboundStrategy: row.inboundStrategy,
    });
    setTrafficStrategy(row.trafficStrategy as TrafficResolutionStrategy);
    setInboundStrategy(row.inboundStrategy as TrafficResolutionStrategy);
    const items: ChainItem[] = (row.profiles || []).map((pi) => {
      const p = profileById.get(pi.id);
      return {
        profileId: pi.id,
        name: pi.name,
        traffic: p?.traffic ?? null,
        expiryDays: p?.expiryDays ?? null,
        limitIp: p?.limitIp ?? null,
        inboundIds: p?.inboundIds ?? [],
      };
    });
    setChain(items);
  }, [methods, profileById]);

  useEffect(() => {
    if (!open) return;
    if (editingTariff) initFromTariff(editingTariff);
    else resetForm();
  }, [open, editingTariff, initFromTariff, resetForm]);

  const availableProfiles = useMemo(() => {
    const added = new Set(chain.map((c) => c.profileId));
    const search = profileSearch.toLowerCase();
    return profiles.filter((p) => !added.has(p.id) && p.name.toLowerCase().includes(search));
  }, [profiles, chain, profileSearch]);

  function addProfile(id: number) {
    const p = profileById.get(id);
    if (!p) return;
    setChain([
      ...chain,
      {
        profileId: p.id,
        name: p.name,
        traffic: p.traffic,
        expiryDays: p.expiryDays,
        limitIp: p.limitIp,
        inboundIds: p.inboundIds,
      },
    ]);
    setProfileSearch('');
  }

  return (
    <Modal
      open={open}
      title={readonly ? t('pages.tariffs.viewTariff') : (editingTariff ? t('pages.tariffs.editTariff') : t('pages.tariffs.addTariff'))}
      okText={editingTariff ? t('pages.clients.submitEdit') : t('create')}
      cancelText={readonly ? t('close') : t('close')}
      okButtonProps={{ loading: saving, style: readonly ? { display: 'none' } : undefined }}
      cancelButtonProps={readonly ? { style: { display: 'none' } } : undefined}
      onOk={readonly ? onClose : methods.handleSubmit(async (values) => {
        await onSave!(
          { ...values, tariffId: editingTariff?.id, trafficStrategy, inboundStrategy },
          chain.map((c, i) => ({ id: c.profileId, position: i })),
        );
        onClose();
      })}
      onCancel={onClose}
      width={680}
    >
      <FormProvider {...methods}>
        <form>
          <FormField name="name" label={t('pages.tariffs.name')} rules={{ required: t('pages.tariffs.name') }}>
            <Input placeholder={t('pages.tariffs.namePlaceholder')} disabled={readonly} />
          </FormField>

          {!readonly && editingTariff && (editingTariff.groupCount || 0) > 0 && (
            <div
              style={{
                background: 'var(--ant-color-warning-bg)',
                border: '1px solid var(--ant-color-warning-border)',
                borderRadius: 6,
                padding: '8px 12px',
                marginBottom: 16,
                fontSize: 13,
              }}
            >
              <Typography.Text>
                {t('pages.tariffs.impactNotice', {
                  groups: editingTariff.groupCount ?? 0,
                  clients: editingTariff.clientCount ?? 0,
                })}
              </Typography.Text>
            </div>
          )}

          <div style={{ marginBottom: 16 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
              <Typography.Text strong>{t('pages.tariffs.profileChain')}</Typography.Text>
              {!readonly && (
              <Popover
                open={addPopoverOpen}
                onOpenChange={setAddPopoverOpen}
                trigger="click"
                placement="bottomRight"
                content={
                  <div style={{ width: 320 }}>
                    <Input
                      size="small"
                      prefix={<SearchOutlined />}
                      placeholder={t('pages.tariffs.addProfile')}
                      value={profileSearch}
                      onChange={(e) => setProfileSearch(e.target.value)}
                      style={{ marginBottom: 8 }}
                      allowClear
                    />
                    {availableProfiles.length === 0 ? (
                      <Typography.Text type="secondary" style={{ display: 'block', padding: 8, textAlign: 'center' }}>
                        {profileSearch ? t('pages.tariffs.noProfilesFound') : t('pages.tariffs.allProfilesAdded')}
                      </Typography.Text>
                    ) : (
                      <List
                        size="small"
                        style={{ maxHeight: 240, overflow: 'auto' }}
                        dataSource={availableProfiles}
                        renderItem={(p) => (
                          <List.Item
                            actions={[
                              <Button size="small" type="link" onClick={() => addProfile(p.id)}>
                                {t('pages.tariffs.addProfile')}
                              </Button>,
                            ]}
                          >
                            <List.Item.Meta
                              title={p.name}
                              description={
                                <span>
                                  {p.traffic != null && `${p.traffic} GB`}
                                  {p.expiryDays != null && ` · ${p.expiryDays}d`}
                                  {p.limitIp != null && ` · ${p.limitIp} IP`}
                                </span>
                              }
                            />
                          </List.Item>
                        )}
                      />
                    )}
                  </div>
                }
              >
                <Button size="small" icon={<PlusOutlined />}>{t('pages.tariffs.addProfile')}</Button>
              </Popover>
              )}
            </div>
            {chain.length === 0 ? (
              <Typography.Text type="secondary" style={{ display: 'block', padding: '12px 0', textAlign: 'center' }}>
                {t('pages.tariffs.noProfilesInChain')}
              </Typography.Text>
            ) : (
              <Card size="small" styles={{ body: { padding: 0 } }}>
                {chain.map((item, i) => {
                  const labels: string[] = [];
                  if (item.traffic != null) labels.push(`${item.traffic} GB`);
                  if (item.expiryDays != null) labels.push(`${item.expiryDays}d`);
                  if (item.limitIp != null) labels.push(`${item.limitIp} IP`);
                  const inboundNames = item.inboundIds.map((id) => inboundLabelById.get(id) || `#${id}`);
                  if (inboundNames.length === 1) {
                    labels.push(inboundNames[0]);
                  } else if (inboundNames.length === 2) {
                    labels.push(inboundNames.join(', '));
                  } else if (inboundNames.length > 2) {
                    labels.push(`${inboundNames.slice(0, 2).join(', ')} +${inboundNames.length - 2}`);
                  }
                  const summary = labels.length > 0 ? labels.join(', ') : '—';
                  const tooltip = inboundNames.length > 2 ? inboundNames.join('\n') : summary;
                  return (
                    <div key={i}
                      style={{
                        display: 'flex', alignItems: 'center',
                        padding: '6px 12px',
                        borderBottom: i < chain.length - 1 ? undefined : 'none',
                      }}
                    >
                      <Typography.Text type="secondary" style={{ width: 24, flexShrink: 0 }}>{i}</Typography.Text>
                      <Typography.Text strong style={{ width: 140, flexShrink: 0 }}>{item.name}</Typography.Text>
                      <Tooltip title={<div style={{ whiteSpace: 'pre-line' }}>{tooltip}</div>}>
                        <Typography.Text type="secondary" style={{ fontSize: 13, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: 1 }}>
                          {summary}
                        </Typography.Text>
                      </Tooltip>
                      {!readonly && (
                      <Space size={2} style={{ flexShrink: 0 }}>
                        <Button size="small" type="text" icon={<ArrowUpOutlined />} disabled={i === 0}
                          onClick={() => {
                            const next = [...chain];
                            [next[i - 1], next[i]] = [next[i], next[i - 1]];
                            setChain(next);
                          }} />
                        <Button size="small" type="text" icon={<ArrowDownOutlined />} disabled={i === chain.length - 1}
                          onClick={() => {
                            const next = [...chain];
                            [next[i], next[i + 1]] = [next[i + 1], next[i]];
                            setChain(next);
                          }} />
                        <Button size="small" type="text" danger icon={<DeleteOutlined />}
                          onClick={() => setChain(chain.filter((_, j) => j !== i))} />
                      </Space>
                      )}
                    </div>
                  );
                })}
              </Card>
            )}
          </div>

          {chain.length > 0 && (
            <Card size="small" title={t('pages.tariffs.resolvedPreview')} style={{ marginBottom: 16 }}>
              <table style={{ width: '100%', borderCollapse: 'collapse' }}>
                <tbody>
                  <tr>
                    <td style={{ padding: '4px 8px', width: 120 }}>
                      <Typography.Text type="secondary">{t('pages.profiles.traffic')}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px', fontWeight: 500 }}>
                      <div>{resolved.trafficBytes > 0 ? `${(resolved.trafficBytes / bytesPerGB).toFixed(0)} GB` : '∞'}</div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t('pages.tariffs.fromSource', { source: resolved.trafficSource })}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px', width: 200 }}>
                      {readonly ? (
                        <Tag style={{ margin: 0 }}>{t(`pages.tariffs.${trafficStrategy}`)}</Tag>
                      ) : (
                      <Select size="small" style={{ width: '100%' }}
                        value={trafficStrategy}
                        onChange={setTrafficStrategy}
                        options={[
                          { value: TrafficResolutionStrategy.Overwrite, label: t('pages.tariffs.overwrite') },
                          { value: TrafficResolutionStrategy.Sum, label: t('pages.tariffs.sum') },
                        ]}
                      />
                      )}
                    </td>
                    <td style={{ padding: '4px 8px', width: 24 }}>
                      <Tooltip title={t('pages.tariffs.trafficStrategyHint')}>
                        <InfoCircleOutlined style={{ color: 'var(--ant-color-text-quaternary)' }} />
                      </Tooltip>
                    </td>
                  </tr>
                  <tr>
                    <td style={{ padding: '4px 8px' }}>
                      <Typography.Text type="secondary">{t('pages.profiles.expiryDays')}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px', fontWeight: 500 }}>
                      <div>{resolved.expiryDays > 0 ? `${resolved.expiryDays} d` : t('pages.tariffs.neverPlaceholder')}</div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t('pages.tariffs.fromSource', { source: resolved.expirySource })}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px' }}>
                      <Tag style={{ margin: 0 }}>{t('pages.tariffs.overwrite')}</Tag>
                    </td>
                    <td style={{ padding: '4px 8px' }}>
                      <Tooltip title={t('pages.tariffs.expiryStrategyHint')}>
                        <InfoCircleOutlined style={{ color: 'var(--ant-color-text-quaternary)' }} />
                      </Tooltip>
                    </td>
                  </tr>
                  <tr>
                    <td style={{ padding: '4px 8px' }}>
                      <Typography.Text type="secondary">{t('pages.profiles.limitIp')}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px', fontWeight: 500 }}>
                      <div>{resolved.limitIp > 0 ? resolved.limitIp : '∞'}</div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t('pages.tariffs.fromSource', { source: resolved.ipSource })}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px' }}>
                      <Tag style={{ margin: 0 }}>{t('pages.tariffs.overwrite')}</Tag>
                    </td>
                    <td style={{ padding: '4px 8px' }}>
                      <Tooltip title={t('pages.tariffs.ipStrategyHint')}>
                        <InfoCircleOutlined style={{ color: 'var(--ant-color-text-quaternary)' }} />
                      </Tooltip>
                    </td>
                  </tr>
                  <tr>
                    <td style={{ padding: '4px 8px' }}>
                      <Typography.Text type="secondary">{t('pages.profiles.inbounds')}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px', fontWeight: 500 }}>
                      <div>
                        {resolved.inboundIds.length > 0
                          ? resolved.inboundIds.map((id) => inboundLabelById.get(id) || `#${id}`).join(', ')
                          : '—'}
                      </div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>{t('pages.tariffs.fromSource', { source: resolved.inboundSource })}</Typography.Text>
                    </td>
                    <td style={{ padding: '4px 8px', width: 200 }}>
                      {readonly ? (
                        <Tag style={{ margin: 0 }}>{t(`pages.tariffs.${inboundStrategy}`)}</Tag>
                      ) : (
                      <Select size="small" style={{ width: '100%' }}
                        value={inboundStrategy}
                        onChange={setInboundStrategy}
                        options={[
                          { value: TrafficResolutionStrategy.Overwrite, label: t('pages.tariffs.overwrite') },
                          { value: TrafficResolutionStrategy.Union, label: t('pages.tariffs.union') },
                        ]}
                      />
                      )}
                    </td>
                    <td style={{ padding: '4px 8px', width: 24 }}>
                      <Tooltip title={t('pages.tariffs.inboundStrategyHint')}>
                        <InfoCircleOutlined style={{ color: 'var(--ant-color-text-quaternary)' }} />
                      </Tooltip>
                    </td>
                  </tr>
                </tbody>
              </table>
            </Card>
          )}
        </form>
      </FormProvider>
    </Modal>
  );
}
