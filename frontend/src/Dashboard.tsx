import React, { useEffect, useState } from 'react';
import { QuestionCircleOutlined } from '@ant-design/icons';
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Checkbox,
  Col,
  Descriptions,
  Empty,
  Form,
  Input,
  InputNumber,
  Layout,
  Row,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { ACCENT } from './theme';
import {
  api,
  type DesktopDefaults,
  type JDAutomationOptions,
  type JDAutomationStatus,
  type LicenseState,
  type NotifyConfig,
  type RequestLogEntry,
  type SKUEntry,
  type SKUSnapshot,
  type Status,
} from './wails';

const emptyStatus: Status = {
  proxyRunning: false,
  addr: '127.0.0.1:8899',
  rulesPath: 'configs/jd.rules.json',
  systemProxyActive: false,
  rootCertPath: '',
  rootThumbprint: '',
  rootTrusted: false,
  baseDir: '',
  logDir: '',
  proxyStatePath: '',
  licensed: false,
  lastError: '',
};

const defaultJDOptions: JDAutomationOptions = {
  processName: 'WeChatAppEx',
  windowTitleContains: '京东',
  inputMode: 'background',
  repeatCount: 1,
  cartTabXRatio: 0.7,
  cartTabYRatio: 0.95,
  allTabXRatio: 0.1,
  allTabYRatio: 0.108,
  serviceTabXRatio: 0.62,
  serviceTabYRatio: 0.108,
  firstDelaySeconds: 30,
};

const emptyJDStatus: JDAutomationStatus = {
  running: false,
  currentCycle: 0,
  totalCycles: 0,
  lastError: '',
};

const defaultNotifyConfig: NotifyConfig = {
  enabled: true,
  dingtalk: { enabled: false, webhookUrl: '', secret: '' },
  bark: { enabled: false, serverUrl: 'https://api.day.app', deviceKey: '' },
  categories: { price: true, stock: true, promo: true, gift: true },
  stockChangeThreshold: 5,
  showProductUrl: true,
  showCheckoutUrl: false,
  showAppQrCode: true,
  quoteDiffFilterEnabled: false,
  quoteDiffThreshold: 0,
  discountRate: 0.97,
  format: 'markdown',
  title: '京东购物车价格变动',
  template: '',
};

type DashboardProps = {
  licenseState: LicenseState;
  licenseBusy: boolean;
  onDeactivateLicense: () => Promise<void>;
};

export default function Dashboard({ licenseState, licenseBusy, onDeactivateLicense }: DashboardProps) {
  const [status, setStatus] = useState<Status>(emptyStatus);
  const [defaults, setDefaults] = useState<DesktopDefaults>({
    rulesPath: 'configs/jd.rules.json',
    automationPath: 'configs/example.automation.json',
    proxyAddr: '127.0.0.1:8899',
    proxyOverride: 'localhost;127.0.0.1;<local>',
  });
  const [addr, setAddr] = useState('127.0.0.1:8899');
  const [rulesPath, setRulesPath] = useState('configs/jd.rules.json');
  const [proxyOverride, setProxyOverride] = useState('localhost;127.0.0.1;<local>');
  const [jdOptions, setJdOptions] = useState<JDAutomationOptions>(defaultJDOptions);
  const [jdStatus, setJdStatus] = useState<JDAutomationStatus>(emptyJDStatus);
  const [requestLogs, setRequestLogs] = useState<RequestLogEntry[]>([]);
  const [skuList, setSkuList] = useState<SKUEntry[]>([]);
  const [showOnlyChanged, setShowOnlyChanged] = useState(false);
  const [showOnlyPriceDrop, setShowOnlyPriceDrop] = useState(false);
  const [showOnlyInStock, setShowOnlyInStock] = useState(false);
  const [skuKeyword, setSkuKeyword] = useState('');
  const [skuSortBy, setSkuSortBy] = useState<'default' | 'dropDesc' | 'finalAsc'>('default');
  const [skuMeta, setSkuMeta] = useState({ parseCount: 0, totalSku: 0, updatedAt: '' });
  const [busy, setBusy] = useState(false);
  const [notifyConfig, setNotifyConfig] = useState<NotifyConfig>(defaultNotifyConfig);
  const { message } = AntApp.useApp();

  useEffect(() => {
    void loadAppState();
  }, []);

  useEffect(() => {
    if (!status.proxyRunning) return;
    const timer = window.setInterval(async () => {
      try {
        setRequestLogs(await api().GetRequestLogs());
      } catch {
        // Ignore transient polling errors.
      }
    }, 1500);
    return () => window.clearInterval(timer);
  }, [status.proxyRunning]);

  useEffect(() => {
    if (!status.proxyRunning) return;
    const timer = window.setInterval(async () => {
      try {
        applySkuSnapshot(await api().GetSKUList());
      } catch {
        // Ignore transient polling errors.
      }
    }, 2000);
    return () => window.clearInterval(timer);
  }, [status.proxyRunning]);

  useEffect(() => {
    if (!jdStatus.running) return;
    const timer = window.setInterval(async () => {
      try {
        setJdStatus(await api().GetJDAutomationStatus());
      } catch {
        // Ignore transient polling errors.
      }
    }, 1000);
    return () => window.clearInterval(timer);
  }, [jdStatus.running]);

  async function loadAppState() {
    await runTask(async () => {
      const loadedDefaults = await api().GetDefaults();
      setDefaults(loadedDefaults);
      setAddr(loadedDefaults.proxyAddr);
      setRulesPath(loadedDefaults.rulesPath);
      setProxyOverride(loadedDefaults.proxyOverride);
      setStatus(await api().GetStatus());
      try {
        applySkuSnapshot(await api().GetSKUList());
      } catch {
        // SKU list is optional for older backends.
      }
      try {
        setNotifyConfig(normalizeNotifyConfig(await api().GetNotifyConfig()));
      } catch {
        // Notification config is optional for older backends.
      }
    }, '已就绪');
  }

  async function runTask(task: () => Promise<void>, successMessage?: string) {
    setBusy(true);
    try {
      await task();
      if (successMessage) message.success(successMessage);
    } catch (error) {
      message.error(error instanceof Error ? error.message : String(error));
    } finally {
      setBusy(false);
    }
  }

  async function startProxy() {
    await runTask(async () => {
      setRequestLogs([]);
      setStatus(await api().StartProxy({ addr, rulesPath, enableSystemProxy: true, proxyOverride }));
    }, '代理已启动，已自动设置系统代理并检查证书');
  }

  async function stopProxy() {
    await runTask(async () => {
      setStatus(await api().StopProxy());
    }, '代理已停止');
  }

  function applySkuSnapshot(snapshot: SKUSnapshot) {
    setSkuList(snapshot.entries ?? []);
    setSkuMeta({ parseCount: snapshot.parseCount, totalSku: snapshot.totalSku, updatedAt: snapshot.updatedAt });
  }

  async function resetSkuList() {
    await runTask(async () => {
      applySkuSnapshot(await api().ResetSKUList());
    }, 'SKU 列表已清空');
  }

  async function installCert() {
    await runTask(async () => {
      setStatus(await api().InstallCert());
    }, '证书已安装');
  }

  async function uninstallCert() {
    await runTask(async () => {
      setStatus(await api().UninstallCert());
    }, '证书已卸载');
  }

  async function saveNotifyConfig() {
    await runTask(async () => {
      await api().SaveNotifyConfig(notifyConfig);
    }, '通知配置已保存并立即生效');
  }

  async function testNotify() {
    await runTask(async () => {
      await api().TestNotify(notifyConfig);
    }, '测试消息已发送，请到钉钉群查看');
  }

  async function toggleJDAutomation(enabled: boolean) {
    if (!enabled) {
      await runTask(async () => {
        setJdStatus(await api().StopJDAutomation());
      }, '京东自动化已停止');
      return;
    }
    setBusy(true);
    try {
      setJdStatus(await api().StartJDAutomation(jdOptions));
      message.success(
        jdOptions.inputMode === 'background'
          ? '京东自动化已在后台启动，可继续使用鼠标和其他窗口'
          : '京东自动化已启动，运行期间请勿移动鼠标或切换窗口',
      );
    } catch (error) {
      const detail = error instanceof Error ? error.message : String(error);
      message.error(
        `无法启动京东自动化：${detail}。请确认目标窗口标题包含“${jdOptions.windowTitleContains}”，宿主进程为“${jdOptions.processName}”${jdOptions.inputMode === 'background' ? '，且窗口未最小化' : ''}。`,
      );
      try {
        setJdStatus(await api().GetJDAutomationStatus());
      } catch {
        // Ignore a follow-up status failure.
      }
    } finally {
      setBusy(false);
    }
  }

  const jdInfinite = jdOptions.repeatCount <= 0;
  const normalizedKeyword = skuKeyword.trim().toLowerCase();
  const filteredSkuList = skuList.filter((entry) => {
    if (showOnlyChanged && !entry.priceChanged) return false;
    if (showOnlyPriceDrop && !(entry.priceChanged && entry.finalDeltaCents < 0)) return false;
    if (showOnlyInStock && (entry.stockCode === 1 || entry.stockDesc.includes('无货'))) return false;
    if (normalizedKeyword) {
      const haystack = `${entry.name} ${entry.itemId} ${entry.vendorName} ${entry.vendorId}`.toLowerCase();
      if (!haystack.includes(normalizedKeyword)) return false;
    }
    return true;
  }).sort((left, right) => {
    if (skuSortBy === 'dropDesc') {
      const leftDrop = left.finalDeltaCents < 0 ? Math.abs(left.finalDeltaCents) : 0;
      const rightDrop = right.finalDeltaCents < 0 ? Math.abs(right.finalDeltaCents) : 0;
      if (leftDrop !== rightDrop) return rightDrop - leftDrop;
      return right.updateCount - left.updateCount;
    }
    if (skuSortBy === 'finalAsc') {
      if (left.finalPriceCents !== right.finalPriceCents) return left.finalPriceCents - right.finalPriceCents;
      return right.updateCount - left.updateCount;
    }
    return 0;
  });

  const proxyItems = [
    { key: 'p1', label: '代理状态', children: <Tag color={status.proxyRunning ? 'green' : 'default'}>{status.proxyRunning ? '运行中' : '已停止'}</Tag> },
    { key: 'p2', label: '系统代理', children: <Tag color={status.systemProxyActive ? 'green' : 'default'}>{status.systemProxyActive ? '已启用' : '未启用'}</Tag> },
    { key: 'p3', label: '证书信任', children: <Tag color={status.rootTrusted ? 'green' : 'orange'}>{status.rootTrusted ? '已安装' : '未安装'}</Tag> },
  ];

  const logColumns: ColumnsType<RequestLogEntry> = [
    { title: '时间', dataIndex: 'time', width: 96, render: (value: string) => new Date(value).toLocaleTimeString() },
    { title: '方法', dataIndex: 'method', width: 78, render: (value: string) => <Tag color="cyan">{value}</Tag> },
    { title: '动作', dataIndex: 'actionType', width: 108, render: (value?: string) => <Tag color="gold">{value || 'mock'}</Tag> },
    { title: '状态', dataIndex: 'status', width: 70, render: (value?: number) => <Tag color={value && value >= 400 ? 'red' : 'default'}>{value ?? '-'}</Tag> },
    { title: '规则', dataIndex: 'ruleName', width: 160, ellipsis: true, render: (value?: string) => value || '未命名规则' },
    { title: 'URL', dataIndex: 'url', ellipsis: true },
  ];

  const skuColumns: ColumnsType<SKUEntry> = [
    {
      title: '商品',
      dataIndex: 'name',
      ellipsis: true,
      render: (_value: string, entry) => (
        <div>
          <div style={{ fontWeight: 600 }}>{entry.name || entry.itemId}</div>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>{entry.vendorName || entry.vendorId || '未知店铺'}</Typography.Text>
        </div>
      ),
    },
    {
      title: '到手价',
      dataIndex: 'finalPriceCents',
      width: 132,
      render: (value: number, entry) => (
        <Space size={4}>
          <span style={{ fontWeight: 600 }}>{formatYuan(value)}</span>
          {entry.discountCents > 0 && <Tag color="green">省{formatYuan(entry.discountCents)}</Tag>}
        </Space>
      ),
    },
    {
      title: '变化',
      key: 'change',
      width: 230,
      render: (_value, entry) => {
        const tags: React.ReactNode[] = [];
        if (entry.priceChanged) tags.push(<Tag key="price" color={entry.finalDeltaCents < 0 ? 'green' : 'red'}>价格{entry.finalDeltaCents < 0 ? '降' : '涨'}{formatYuan(Math.abs(entry.finalDeltaCents))}</Tag>);
        if (entry.stockChanged) tags.push(<Tag key="stock" color="blue">库存</Tag>);
        if (entry.promoChanged) tags.push(<Tag key="promo" color="gold">优惠</Tag>);
        if (entry.giftChanged) tags.push(<Tag key="gift" color="purple">赠品</Tag>);
        return tags.length ? <Space size={[0, 4]} wrap>{tags}</Space> : <Typography.Text type="secondary">—</Typography.Text>;
      },
    },
    { title: '库存', dataIndex: 'stockDesc', width: 88, render: (value?: string) => value ? <Tag>{value}</Tag> : <Typography.Text type="secondary">—</Typography.Text> },
    { title: '次数', dataIndex: 'updateCount', width: 66, render: (value: number) => `×${value}` },
  ];

  const certItems = [
    { key: 'c1', label: '证书信任', children: <Tag color={status.rootTrusted ? 'green' : 'orange'}>{status.rootTrusted ? '已信任' : '未信任'}</Tag> },
    { key: 'c2', label: '证书指纹', children: <Typography.Text copyable={!!status.rootThumbprint} style={{ fontSize: 12 }}>{status.rootThumbprint || '待生成'}</Typography.Text> },
    { key: 'c3', label: '证书路径', children: <Typography.Text style={{ fontSize: 12 }}>{status.rootCertPath || '待生成'}</Typography.Text> },
    { key: 'c4', label: '应用数据目录', children: <Typography.Text style={{ fontSize: 12 }}>{status.baseDir || '待生成'}</Typography.Text> },
    { key: 'c5', label: '日志目录', children: <Typography.Text style={{ fontSize: 12 }}>{status.logDir || '待生成'}</Typography.Text> },
    { key: 'c6', label: '默认值', children: <Typography.Text style={{ fontSize: 12 }}>{`${defaults.proxyAddr} · ${defaults.rulesPath}`}</Typography.Text> },
  ];

  const expiryDate = licenseState.expiresAt ? new Date(licenseState.expiresAt) : null;
  const expiryTag = expiryDate && !Number.isNaN(expiryDate.getTime()) ? (
    <Tag color="success" style={{ fontWeight: 600 }} title="授权到期时间">
      授权至 {expiryDate.toLocaleDateString('zh-CN')}
    </Tag>
  ) : <Tag color="success" style={{ fontWeight: 600 }}>已激活</Tag>;

  return (
    <Layout className="app-shell">
      <Layout.Header className="app-header">
        <div>
          <Typography.Text className="eyebrow">MINI PROXY 桌面端</Typography.Text>
          <Typography.Title level={3} style={{ margin: 0 }}>小程序自动化控制台</Typography.Title>
        </div>
        <Space>
          {expiryTag}
          <Button size="small" danger type="link" onClick={() => void onDeactivateLicense()} disabled={busy || licenseBusy} style={{ padding: 0 }}>停用授权</Button>
          <Tag color={status.proxyRunning ? 'green' : 'default'} style={{ fontWeight: 700, padding: '4px 12px', fontSize: 13 }}>
            {status.proxyRunning ? '● 代理运行中' : '○ 代理已停止'}
          </Tag>
        </Space>
      </Layout.Header>

      <Layout.Content>
        <Row gutter={[16, 16]}>
          <Col xs={24} lg={12}>
            <Card title={<CardTitle color={ACCENT.mint}>代理控制</CardTitle>}>
              <Descriptions column={1} size="small" bordered items={proxyItems} />
              <div style={{ marginTop: 16 }}>
                {status.proxyRunning ? (
                  <Button danger type="primary" block loading={busy} onClick={() => void stopProxy()}>停止代理</Button>
                ) : (
                  <Button type="primary" block loading={busy} onClick={() => void startProxy()}>启动代理</Button>
                )}
              </div>
              <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0, fontSize: 12 }}>
                启动时自动设置 Windows 系统代理，并检查/安装本地根证书。规则文件：{rulesPath}
              </Typography.Paragraph>
            </Card>
          </Col>

          <Col xs={24} lg={12}>
            <Card title={(
              <CardTitle color={ACCENT.coral}>
                京东小程序自动化
                <Tooltip title={jdOptions.inputMode === 'background'
                  ? '需保持京东小程序窗口打开且未最小化，可以被其他窗口遮挡。后台模式不会主动切换前台窗口或移动真实鼠标。仅在购物车“全部/服务”页签间切换。'
                  : '需先手动打开京东小程序窗口。前台兼容模式会切换窗口并操控鼠标，请勿同时移动鼠标或切换窗口。仅在购物车“全部/服务”页签间切换。'}>
                  <QuestionCircleOutlined aria-label="自动化运行提示" style={{ color: ACCENT.coral, cursor: 'help' }} />
                </Tooltip>
              </CardTitle>
            )}>
              <Form layout="vertical" size="small" disabled={jdStatus.running || busy}>
                <Row gutter={12}>
                  <Col span={24}>
                    <Form.Item label="操作方式" style={{ marginBottom: 12 }}>
                      <Segmented
                        block
                        value={jdOptions.inputMode}
                        options={[
                          { label: '后台静默', value: 'background' },
                          { label: '前台兼容', value: 'foreground' },
                        ]}
                        onChange={(value) => setJdOptions({ ...jdOptions, inputMode: value as JDAutomationOptions['inputMode'] })}
                      />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="窗口标题包含" style={{ marginBottom: 12 }}>
                      <Input value={jdOptions.windowTitleContains} onChange={(event) => setJdOptions({ ...jdOptions, windowTitleContains: event.target.value })} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="宿主进程名" style={{ marginBottom: 12 }}>
                      <Input value={jdOptions.processName} onChange={(event) => setJdOptions({ ...jdOptions, processName: event.target.value })} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="循环次数" style={{ marginBottom: 12 }}>
                      <InputNumber style={{ width: '100%' }} min={1} disabled={jdInfinite || jdStatus.running || busy} value={jdInfinite ? undefined : jdOptions.repeatCount} onChange={(value) => setJdOptions({ ...jdOptions, repeatCount: Number(value ?? 1) })} />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item label="在“全部”停留秒数" style={{ marginBottom: 12 }}>
                      <InputNumber style={{ width: '100%' }} min={0} value={jdOptions.firstDelaySeconds} onChange={(value) => setJdOptions({ ...jdOptions, firstDelaySeconds: Number(value ?? 0) })} />
                    </Form.Item>
                  </Col>
                </Row>
                <Form.Item style={{ marginBottom: 0 }}>
                  <Space>
                    <Switch checked={jdInfinite} onChange={(checked) => setJdOptions({ ...jdOptions, repeatCount: checked ? 0 : 1 })} />
                    <span>无限循环（一直运行，直到手动停止）</span>
                  </Space>
                </Form.Item>
              </Form>
              <Space style={{ marginTop: 12 }} wrap>
                <Button type="primary" loading={busy} disabled={jdStatus.running || busy} onClick={() => void toggleJDAutomation(true)}>开始</Button>
                <Button disabled={!jdStatus.running || busy} onClick={() => void toggleJDAutomation(false)}>关闭</Button>
                <Tag color={jdStatus.running ? 'green' : 'default'}>
                  {jdStatus.running ? jdStatus.totalCycles > 0 ? `运行中 ${jdStatus.currentCycle}/${jdStatus.totalCycles}` : `运行中 第${jdStatus.currentCycle}次 · 无限` : '已停止'}
                </Tag>
              </Space>
              {jdStatus.lastError && <Alert style={{ marginTop: 12 }} type="error" showIcon message={jdStatus.lastError} />}
            </Card>
          </Col>

          <Col span={24}>
            <Card title={<CardTitle color={ACCENT.ink}>代理请求日志</CardTitle>} extra={(
              <Space>
                <Button size="small" disabled={busy} onClick={() => void api().GetRequestLogs().then(setRequestLogs)}>刷新</Button>
                <Button size="small" disabled={busy} onClick={() => setRequestLogs([])}>清空显示</Button>
              </Space>
            )}>
              <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12, marginBottom: 8 }}>
                {status.proxyRunning ? `已拦截 ${requestLogs.length} 条请求（自动刷新）` : '代理未运行，显示的是最近一次运行的拦截记录'}
              </Typography.Text>
              <Table size="small" rowKey={(record) => `${record.time}-${record.url}`} columns={logColumns} dataSource={[...requestLogs].reverse()} pagination={{ pageSize: 8, size: 'small', hideOnSinglePage: true }} locale={{ emptyText: <Empty description="暂无拦截记录，命中规则的请求会显示在这里。" /> }} scroll={{ x: 720 }} />
            </Card>
          </Col>

          <Col span={24}>
            <Card title={<CardTitle color={ACCENT.violet}>京东购物车 SKU</CardTitle>} extra={(
              <Space>
                <Button size="small" disabled={busy} onClick={() => void api().GetSKUList().then(applySkuSnapshot)}>刷新</Button>
                <Button size="small" danger disabled={busy} onClick={() => void resetSkuList()}>清空</Button>
              </Space>
            )}>
              <Space wrap style={{ marginBottom: 8 }}>
                <Checkbox checked={showOnlyChanged} onChange={(event) => setShowOnlyChanged(event.target.checked)}>仅看价格变化</Checkbox>
                <Checkbox checked={showOnlyPriceDrop} onChange={(event) => setShowOnlyPriceDrop(event.target.checked)}>仅看降价</Checkbox>
                <Checkbox checked={showOnlyInStock} onChange={(event) => setShowOnlyInStock(event.target.checked)}>仅看有货</Checkbox>
                <Select size="small" style={{ width: 150 }} value={skuSortBy} onChange={(value) => setSkuSortBy(value)} options={[
                  { value: 'default', label: '默认顺序' },
                  { value: 'dropDesc', label: '按降价幅度' },
                  { value: 'finalAsc', label: '按到手价升序' },
                ]} />
                <Input.Search size="small" allowClear style={{ width: 240 }} placeholder="按商品名 / SKU / 店铺筛选" value={skuKeyword} onChange={(event) => setSkuKeyword(event.target.value)} />
              </Space>
              <Typography.Text type="secondary" style={{ display: 'block', fontSize: 12, marginBottom: 8 }}>
                {`已解析 ${skuMeta.parseCount} 次 · 共 ${skuMeta.totalSku} 个 SKU · 当前显示 ${filteredSkuList.length} 个`}
                {skuMeta.updatedAt ? ` · 更新于 ${new Date(skuMeta.updatedAt).toLocaleTimeString()}` : ''}
              </Typography.Text>
              <Table size="small" rowKey="itemId" columns={skuColumns} dataSource={filteredSkuList} pagination={{ pageSize: 10, size: 'small', hideOnSinglePage: true }} locale={{ emptyText: <Empty description={skuList.length === 0 ? '暂无 SKU。命中 extract 规则的京东购物车响应会解析并显示在这里。' : '当前筛选条件下没有结果，请放宽条件后再试。'} /> }} scroll={{ x: 560 }} />
              <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0, fontSize: 12 }}>
                价格为「到手价」，含 landedPrice 时按其计算，否则回退到页面价。降/涨标签对比上一次抓取到的到手价。
              </Typography.Paragraph>
            </Card>
          </Col>

          <Col span={24}>
            <Card title={<CardTitle color={ACCENT.mint}>SKU 变动通知</CardTitle>}>
              <Form layout="vertical" size="small">
                <Form.Item style={{ marginBottom: 12 }}>
                  <Space><Switch checked={notifyConfig.enabled} onChange={(checked) => setNotifyConfig({ ...notifyConfig, enabled: checked })} /><span>启用 SKU 变动通知</span></Space>
                </Form.Item>
                <Row gutter={12}>
                  <Col xs={24} sm={12}>
                    <Form.Item style={{ marginBottom: 8 }}><Space><Switch checked={notifyConfig.dingtalk.enabled} onChange={(enabled) => setNotifyConfig({ ...notifyConfig, dingtalk: { ...notifyConfig.dingtalk, enabled } })} /><span>启用钉钉</span></Space></Form.Item>
                    <Form.Item label="钉钉 Webhook" style={{ marginBottom: 12 }}>
                      <Input value={notifyConfig.dingtalk.webhookUrl} placeholder="https://oapi.dingtalk.com/robot/send?access_token=..." onChange={(event) => setNotifyConfig({ ...notifyConfig, dingtalk: { ...notifyConfig.dingtalk, webhookUrl: event.target.value } })} />
                    </Form.Item>
                    <Form.Item label="钉钉加签 Secret" style={{ marginBottom: 12 }}>
                      <Input.Password value={notifyConfig.dingtalk.secret ?? ''} placeholder="SECxxxxxxxx" onChange={(event) => setNotifyConfig({ ...notifyConfig, dingtalk: { ...notifyConfig.dingtalk, secret: event.target.value } })} />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={12}>
                    <Form.Item style={{ marginBottom: 8 }}><Space><Switch checked={notifyConfig.bark.enabled} onChange={(enabled) => setNotifyConfig({ ...notifyConfig, bark: { ...notifyConfig.bark, enabled } })} /><span>启用 Bark</span></Space></Form.Item>
                    <Form.Item label="Bark 服务器" style={{ marginBottom: 12 }}>
                      <Input value={notifyConfig.bark.serverUrl} placeholder="https://api.day.app" onChange={(event) => setNotifyConfig({ ...notifyConfig, bark: { ...notifyConfig.bark, serverUrl: event.target.value } })} />
                    </Form.Item>
                    <Form.Item label="Bark Device Key" style={{ marginBottom: 12 }}>
                      <Input.Password value={notifyConfig.bark.deviceKey} onChange={(event) => setNotifyConfig({ ...notifyConfig, bark: { ...notifyConfig.bark, deviceKey: event.target.value } })} />
                    </Form.Item>
                  </Col>
                </Row>
                <Form.Item label="通知类别" style={{ marginBottom: 12 }}>
                  <Space wrap>
                    {([['price', '价格'], ['stock', '库存'], ['promo', '优惠'], ['gift', '赠品']] as const).map(([key, label]) => (
                      <Checkbox key={key} checked={notifyConfig.categories[key]} onChange={(event) => setNotifyConfig({ ...notifyConfig, categories: { ...notifyConfig.categories, [key]: event.target.checked } })}>{label}</Checkbox>
                    ))}
                  </Space>
                </Form.Item>
                <Row gutter={12}>
                  <Col xs={24} sm={8}><Form.Item label="库存变化阈值" style={{ marginBottom: 12 }}><InputNumber style={{ width: '100%' }} min={0} step={1} value={notifyConfig.stockChangeThreshold} onChange={(value) => setNotifyConfig({ ...notifyConfig, stockChangeThreshold: Number(value ?? 5) })} /></Form.Item></Col>
                  <Col xs={24} sm={8}><Form.Item label="折扣系数" style={{ marginBottom: 12 }}><InputNumber style={{ width: '100%' }} min={0.001} max={1} step={0.001} value={notifyConfig.discountRate} onChange={(value) => setNotifyConfig({ ...notifyConfig, discountRate: Number(value ?? 0.97) })} /></Form.Item></Col>
                  <Col xs={24} sm={8}><Form.Item label="消息格式" style={{ marginBottom: 12 }}><Select value={notifyConfig.format} onChange={(value) => setNotifyConfig({ ...notifyConfig, format: value })} options={[{ value: 'markdown', label: 'Markdown' }, { value: 'text', label: '纯文本' }]} /></Form.Item></Col>
                </Row>
                <Row gutter={12}>
                  <Col xs={24} sm={12}><Form.Item style={{ marginBottom: 12 }}><Space><Switch checked={notifyConfig.quoteDiffFilterEnabled} onChange={(quoteDiffFilterEnabled) => setNotifyConfig({ ...notifyConfig, quoteDiffFilterEnabled })} /><span>启用报价差价过滤</span></Space></Form.Item></Col>
                  <Col xs={24} sm={12}><Form.Item label="差价阈值（大于才通知）" style={{ marginBottom: 12 }}><InputNumber style={{ width: '100%' }} step={0.01} value={notifyConfig.quoteDiffThreshold} onChange={(value) => setNotifyConfig({ ...notifyConfig, quoteDiffThreshold: Number(value ?? 0) })} /></Form.Item></Col>
                </Row>
                <Form.Item label="通知链接" style={{ marginBottom: 12 }}>
                  <Space wrap>
                    <Checkbox checked={notifyConfig.showProductUrl} onChange={(event) => setNotifyConfig({ ...notifyConfig, showProductUrl: event.target.checked })}>商品链接</Checkbox>
                    <Checkbox checked={notifyConfig.showCheckoutUrl} onChange={(event) => setNotifyConfig({ ...notifyConfig, showCheckoutUrl: event.target.checked })}>支付链接</Checkbox>
                    <Checkbox checked={notifyConfig.showAppQrCode} onChange={(event) => setNotifyConfig({ ...notifyConfig, showAppQrCode: event.target.checked })}>APP&扫码链接</Checkbox>
                  </Space>
                </Form.Item>
                <Form.Item label="标题（仅 Markdown 生效）" style={{ marginBottom: 12 }}>
                  <Input value={notifyConfig.title} placeholder="京东购物车价格变动" onChange={(event) => setNotifyConfig({ ...notifyConfig, title: event.target.value })} />
                </Form.Item>
                <Form.Item label="消息模板（Go 模板，留空使用默认模板）" style={{ marginBottom: 0 }}>
                  <Input.TextArea rows={5} value={notifyConfig.template} placeholder="可用占位符：{{.Name}} {{.ItemID}} {{.FinalYuan}} {{.PrevYuan}} {{.DeltaYuan}} {{.DiscountYuan}} {{.StockDesc}}" onChange={(event) => setNotifyConfig({ ...notifyConfig, template: event.target.value })} />
                </Form.Item>
              </Form>
              <Space style={{ marginTop: 12 }}>
                <Button type="primary" loading={busy} onClick={() => void saveNotifyConfig()}>保存配置</Button>
                <Button loading={busy} onClick={() => void testNotify()}>测试所有已启用渠道</Button>
              </Space>
              <Alert style={{ marginTop: 12 }} type="info" showIcon message="配置保存后立即生效。通知按价格、库存、优惠、赠品分类，每批最多 3 个 SKU；库存数量变化必须严格大于阈值。未匹配报价的商品不受差价过滤影响。" />
            </Card>
          </Col>

          <Col span={24}>
            <Card title={<CardTitle color={ACCENT.gold}>证书与运行时</CardTitle>}>
              <Descriptions column={{ xs: 1, sm: 2 }} size="small" bordered items={certItems} />
              <Space style={{ marginTop: 16 }}>
                <Button disabled={busy} onClick={() => void installCert()}>重新安装证书</Button>
                <Button danger disabled={busy} onClick={() => void uninstallCert()}>卸载证书</Button>
              </Space>
              {status.lastError && <Alert style={{ marginTop: 12 }} type="error" showIcon message={status.lastError} />}
            </Card>
          </Col>
        </Row>
      </Layout.Content>
    </Layout>
  );
}

function CardTitle({ color, children }: { color: string; children: React.ReactNode }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 10 }}>
      <span style={{ width: 10, height: 20, borderRadius: 4, background: color, display: 'inline-block' }} />
      {children}
    </span>
  );
}

function formatYuan(cents: number): string {
  return `¥${(cents / 100).toFixed(2)}`;
}

function normalizeNotifyConfig(config: NotifyConfig): NotifyConfig {
  return {
    enabled: Boolean(config?.enabled),
    dingtalk: {
      enabled: config?.dingtalk?.enabled !== false,
      webhookUrl: config?.dingtalk?.webhookUrl ?? '',
      secret: config?.dingtalk?.secret ?? '',
    },
    bark: {
      enabled: config?.bark?.enabled === true,
      serverUrl: config?.bark?.serverUrl || 'https://api.day.app',
      deviceKey: config?.bark?.deviceKey ?? '',
    },
    categories: {
      price: config?.categories?.price !== false,
      stock: config?.categories?.stock !== false,
      promo: config?.categories?.promo !== false,
      gift: config?.categories?.gift !== false,
    },
    stockChangeThreshold: typeof config?.stockChangeThreshold === 'number' ? config.stockChangeThreshold : 5,
    showProductUrl: config?.showProductUrl !== false,
    showCheckoutUrl: config?.showCheckoutUrl === true,
    showAppQrCode: config?.showAppQrCode !== false,
    quoteDiffFilterEnabled: config?.quoteDiffFilterEnabled === true,
    quoteDiffThreshold: typeof config?.quoteDiffThreshold === 'number' ? config.quoteDiffThreshold : 0,
    discountRate: typeof config?.discountRate === 'number' ? config.discountRate : 0.97,
    format: config?.format || 'markdown',
    title: config?.title || '京东购物车价格变动',
    template: config?.template ?? '',
  };
}
