import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Button,
  Card,
  Col,
  ConfigProvider,
  Layout,
  Result,
  Row,
  Spin,
  Tabs,
  message,
} from 'antd';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { z } from 'zod';

import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { usePageTitle } from '@/hooks/usePageTitle';
import { HttpUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import AppSidebar from '@/layouts/AppSidebar';
import { keys } from '@/api/queryKeys';
import {
  GroupSummaryListSchema,
  type GroupSummary,
} from '@/schemas/client';
import { type Tariff } from '@/schemas/tariff';
import { ProfileSchema, type Profile } from '@/schemas/profile';
import { parseMsg } from '@/utils/zodValidate';
import { formatInboundLabel } from '@/lib/inbounds/label';
import { MULTI_CLIENT_PROTOCOLS } from '@/schemas/primitives/protocol';

import GroupsTab from './GroupsTab';
import TariffsTab from './TariffsTab';
import ProfilesTab from './ProfilesTab';

const ProfileListSchema = z.array(ProfileSchema).nullable().transform((v) => v ?? []);

async function fetchGroups(): Promise<GroupSummary[]> {
  const msg = await HttpUtil.get('/panel/api/clients/groups', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to load groups');
  const validated = parseMsg(msg, GroupSummaryListSchema, 'clients/groups');
  return validated.obj ?? [];
}

async function fetchProfiles(): Promise<Profile[]> {
  const msg = await HttpUtil.get('/panel/api/clients/profiles', undefined, { silent: true });
  if (!msg?.success) throw new Error(msg?.msg || 'Failed to load profiles');
  const validated = parseMsg(msg, ProfileListSchema, 'clients/profiles');
  return validated.obj ?? [];
}

export default function GroupsPage() {
  usePageTitle();
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const { isMobile } = useMediaQuery();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => { setMessageInstance(messageApi); }, [messageApi]);
  const queryClient = useQueryClient();

  const [activeTab, setActiveTab] = useState('groups');

  const groupsQuery = useQuery({
    queryKey: keys.clients.groups(),
    queryFn: fetchGroups,
  });
  const groups = useMemo(() => groupsQuery.data ?? [], [groupsQuery.data]);
  const loading = groupsQuery.isFetching;
  const fetched = groupsQuery.data !== undefined || groupsQuery.isError;
  const fetchError = groupsQuery.error ? (groupsQuery.error as Error).message : '';

  const tariffsQuery = useQuery({
    queryKey: keys.clients.tariffs(),
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/clients/tariffs', undefined, { silent: true });
      if (!msg?.success) return [];
      return (Array.isArray(msg.obj) ? msg.obj : []) as Tariff[];
    },
  });
  const tariffs = useMemo(() => tariffsQuery.data ?? [], [tariffsQuery.data]);

  const profilesQuery = useQuery({
    queryKey: keys.clients.profiles(),
    queryFn: fetchProfiles,
  });
  const profiles = useMemo(() => profilesQuery.data ?? [], [profilesQuery.data]);

  const { data: inbounds = [] } = useQuery({
    queryKey: keys.inbounds.slim(),
    staleTime: 5 * 60 * 1000,
    queryFn: async () => {
      const msg = await HttpUtil.get('/panel/api/inbounds/list', undefined, { silent: true });
      if (!msg?.success) return [];
      return (Array.isArray(msg.obj) ? msg.obj : []) as { id: number; tag: string; remark?: string; protocol?: string }[];
    },
  });

  const inboundOptions = useMemo(() =>
    inbounds
      .filter((ib) => MULTI_CLIENT_PROTOCOLS.has(ib.protocol || ''))
      .map((ib) => ({ value: ib.id, label: formatInboundLabel(ib.tag, ib.remark) })),
    [inbounds],
  );

  const inboundLabelById = useMemo(
    () => new Map(inbounds.map((ib) => [ib.id, formatInboundLabel(ib.tag, ib.remark)] as const)),
    [inbounds],
  );

  const invalidate = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: keys.clients.root() });
  }, [queryClient]);

  const pageClass = useMemo(() => {
    const classes = ['groups-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin spinning={!fetched} delay={200} description={t('loading')} size="large">
              {!fetched ? (
                <div className="loading-spacer" />
              ) : fetchError ? (
                <Result
                  status="error"
                  title={t('somethingWentWrong')}
                  subTitle={fetchError}
                  extra={<Button type="primary" loading={loading} onClick={() => groupsQuery.refetch()}>{t('refresh')}</Button>}
                />
              ) : (
                <Row gutter={[isMobile ? 8 : 16, isMobile ? 8 : 12]}>
                  <Col span={24}>
                    <Card size="small" hoverable>
                      <Tabs
                        activeKey={activeTab}
                        onChange={setActiveTab}
                        items={[
                          {
                            key: 'groups',
                            label: t('pages.groups.title'),
                            children: (
                              <GroupsTab
                                groups={groups}
                                loading={loading}
                                tariffs={tariffs.map((p) => ({ id: p.id, name: p.name }))}
                                invalidate={invalidate}
                              />
                            ),
                          },
                          {
                            key: 'tariffs',
                            label: t('menu.tariffs'),
                            children: (
                              <TariffsTab
                                tariffs={tariffs}
                                loading={tariffsQuery.isFetching}
                                profiles={profiles}
                                inboundLabelById={inboundLabelById}
                                invalidate={invalidate}
                              />
                            ),
                          },
                          {
                            key: 'profiles',
                            label: t('pages.profiles.title'),
                            children: (
                              <ProfilesTab
                                profiles={profiles}
                                loading={profilesQuery.isFetching}
                                inboundOptions={inboundOptions}
                                inboundLabelById={inboundLabelById}
                                invalidate={invalidate}
                              />
                            ),
                          },
                        ]}
                      />
                    </Card>
                  </Col>
                </Row>
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
