import React, { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { api, DesktopDefaults, JDAutomationOptions, JDAutomationStatus, RequestLogEntry, SKUEntry, SKUSnapshot, Status } from './wails';
import './styles.css';

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
  lastError: '',
};

const defaultJDOptions: JDAutomationOptions = {
  processName: 'WeChatAppEx',
  windowTitleContains: '京东',
  repeatCount: 1,
  cartTabXRatio: 0.7,
  cartTabYRatio: 0.95,
  allTabXRatio: 0.1,
  allTabYRatio: 0.108,
  serviceTabXRatio: 0.62,
  serviceTabYRatio: 0.108,
  firstDelaySeconds: 30,
  secondDelaySeconds: 5,
};

const emptyJDStatus: JDAutomationStatus = {
  running: false,
  currentCycle: 0,
  totalCycles: 0,
  lastError: '',
};

function App() {
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
  const [skuMeta, setSkuMeta] = useState<{ parseCount: number; totalSku: number; updatedAt: string }>({
    parseCount: 0,
    totalSku: 0,
    updatedAt: '',
  });
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void initialize();
  }, []);

  useEffect(() => {
    if (!status.proxyRunning) return;
    const timer = window.setInterval(async () => {
      try {
        setRequestLogs(await api().GetRequestLogs());
      } catch {
        // ignore transient polling errors
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
        // ignore transient polling errors
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
        // ignore transient polling errors
      }
    }, 1000);
    return () => window.clearInterval(timer);
  }, [jdStatus.running]);

  async function initialize() {
    await runTask(async () => {
      const loadedDefaults = await api().GetDefaults();
      setDefaults(loadedDefaults);
      setAddr(loadedDefaults.proxyAddr);
      setRulesPath(loadedDefaults.rulesPath);
      setProxyOverride(loadedDefaults.proxyOverride);
      const nextStatus = await api().GetStatus();
      setStatus(nextStatus);
      try {
        applySkuSnapshot(await api().GetSKUList());
      } catch {
        // SKU list is optional; ignore if backend is older
      }
    }, '已就绪');
  }

  async function runTask(task: () => Promise<void>, successMessage?: string) {
    setBusy(true);
    setNotice('');
    try {
      await task();
      if (successMessage) setNotice(successMessage);
    } catch (error) {
      setNotice(error instanceof Error ? error.message : String(error));
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

  async function refreshRequestLogs() {
    setRequestLogs(await api().GetRequestLogs());
  }

  function applySkuSnapshot(snapshot: SKUSnapshot) {
    setSkuList(snapshot.entries ?? []);
    setSkuMeta({ parseCount: snapshot.parseCount, totalSku: snapshot.totalSku, updatedAt: snapshot.updatedAt });
  }

  async function refreshSkuList() {
    applySkuSnapshot(await api().GetSKUList());
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

  async function toggleJDAutomation(enabled: boolean) {
    await runTask(async () => {
      if (enabled) {
        setJdStatus(await api().StartJDAutomation(jdOptions));
      } else {
        setJdStatus(await api().StopJDAutomation());
      }
    }, enabled ? '京东自动化已启动' : '京东自动化已停止');
  }

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

  return (
    <main className="shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">Mini Proxy 桌面端</p>
          <h1>HTTPS 抓包拦截与窗口自动化</h1>
        </div>
        <div className={`status-pill ${status.proxyRunning ? 'running' : ''}`}>
          <span />
          {status.proxyRunning ? '代理运行中' : '代理已停止'}
        </div>
      </section>

      <section className="dashboard-grid">
        <Panel title="代理控制" accent="mint">
          <div className="metric-list">
            <Metric label="代理状态" value={status.proxyRunning ? '运行中' : '已停止'} tone={status.proxyRunning ? 'good' : undefined} />
            <Metric label="系统代理" value={status.systemProxyActive ? '已启用' : '未启用'} tone={status.systemProxyActive ? 'good' : undefined} />
            <Metric label="证书信任" value={status.rootTrusted ? '已安装' : '未安装'} tone={status.rootTrusted ? 'good' : 'warn'} />
          </div>
          <div className="button-row">
            {status.proxyRunning ? (
              <button type="button" className="primary" onClick={stopProxy} disabled={busy}>停止代理</button>
            ) : (
              <button type="button" className="primary" onClick={startProxy} disabled={busy}>启动代理</button>
            )}
          </div>
          <p className="hint">启动时自动设置 Windows 系统代理，并检查/安装本地根证书。规则仅使用文件配置：{rulesPath}</p>
        </Panel>

        <Panel title="京东小程序自动化" accent="coral">
          <div className="field-row two">
            <label>
              窗口标题包含
              <input
                value={jdOptions.windowTitleContains}
                onChange={(event) => setJdOptions({ ...jdOptions, windowTitleContains: event.target.value })}
                disabled={jdStatus.running || busy}
              />
            </label>
            <label>
              宿主进程名
              <input
                value={jdOptions.processName}
                onChange={(event) => setJdOptions({ ...jdOptions, processName: event.target.value })}
                disabled={jdStatus.running || busy}
              />
            </label>
          </div>
          <div className="field-row two">
            <label>
              循环次数
              <input
                type="number"
                min={1}
                value={jdOptions.repeatCount}
                onChange={(event) => setJdOptions({ ...jdOptions, repeatCount: Number(event.target.value) })}
                disabled={jdStatus.running || busy}
              />
            </label>
            <label>
              点"服务"前等待秒数
              <input
                type="number"
                min={0}
                value={jdOptions.firstDelaySeconds}
                onChange={(event) => setJdOptions({ ...jdOptions, firstDelaySeconds: Number(event.target.value) })}
                disabled={jdStatus.running || busy}
              />
            </label>
          </div>
          <label>
            点回"全部"前等待秒数
            <input
              type="number"
              min={0}
              value={jdOptions.secondDelaySeconds}
              onChange={(event) => setJdOptions({ ...jdOptions, secondDelaySeconds: Number(event.target.value) })}
              disabled={jdStatus.running || busy}
            />
          </label>
          <div className="button-row">
            <button type="button" className="primary" onClick={() => void toggleJDAutomation(true)} disabled={jdStatus.running || busy}>开始</button>
            <button type="button" onClick={() => void toggleJDAutomation(false)} disabled={!jdStatus.running || busy}>关闭</button>
          </div>
          <div className="metric-list">
            <Metric
              label="状态"
              value={jdStatus.running ? `运行中 (${jdStatus.currentCycle}/${jdStatus.totalCycles})` : '已停止'}
              tone={jdStatus.running ? 'good' : undefined}
            />
          </div>
          {jdStatus.lastError && <div className="notice">{jdStatus.lastError}</div>}
          <p className="hint">
            需要先手动打开京东小程序窗口。仅在购物车"全部"/"服务"页签间切换，不会确认订单或提交支付。
          </p>
        </Panel>

        <Panel title="代理请求日志" accent="ink" full>
          <div className="editor-toolbar">
            <span className="validation">
              {status.proxyRunning ? `已拦截 ${requestLogs.length} 条请求（自动刷新）` : '代理未运行，显示的是最近一次运行的拦截记录'}
            </span>
            <div className="button-row compact">
              <button type="button" onClick={() => void refreshRequestLogs()} disabled={busy}>刷新</button>
              <button type="button" onClick={() => setRequestLogs([])} disabled={busy}>清空显示</button>
            </div>
          </div>
          <div className="log-list">
            {requestLogs.length === 0 && <div className="log-empty">暂无拦截记录，命中规则的请求会显示在这里。</div>}
            {[...requestLogs].reverse().map((entry, index) => (
              <div className="log-card" key={`${entry.time}-${index}`}>
                <div className="log-card-top">
                  <span className="log-tag log-tag-method">{entry.method}</span>
                  <span className="log-tag log-tag-action">{entry.actionType || 'mock'}</span>
                  <span className="log-tag log-tag-status">{entry.status ?? '-'}</span>
                  <span className="log-time">{new Date(entry.time).toLocaleTimeString()}</span>
                </div>
                <div className="log-card-bottom">
                  <span className="log-rule">{entry.ruleName || '未命名规则'}</span>
                  <span className="log-url" title={entry.url}>{entry.url}</span>
                </div>
              </div>
            ))}
          </div>
        </Panel>

        <Panel title="京东购物车 SKU" accent="violet" full>
          <div className="editor-toolbar">
            <span className="validation">
              {`已解析 ${skuMeta.parseCount} 次 · 共 ${skuMeta.totalSku} 个 SKU · 当前显示 ${filteredSkuList.length} 个`}
              {skuMeta.updatedAt && ` · 更新于 ${new Date(skuMeta.updatedAt).toLocaleTimeString()}`}
            </span>
            <div className="button-row compact">
              <button type="button" onClick={() => void refreshSkuList()} disabled={busy}>刷新</button>
              <button type="button" onClick={() => void resetSkuList()} disabled={busy}>清空</button>
            </div>
          </div>
          <div className="sku-filter-row">
            <label className="toggle-line">
              <input
                type="checkbox"
                checked={showOnlyChanged}
                onChange={(event) => setShowOnlyChanged(event.target.checked)}
              />
              仅看价格变化
            </label>
            <label className="toggle-line">
              <input
                type="checkbox"
                checked={showOnlyPriceDrop}
                onChange={(event) => setShowOnlyPriceDrop(event.target.checked)}
              />
              仅看降价
            </label>
            <label className="toggle-line">
              <input
                type="checkbox"
                checked={showOnlyInStock}
                onChange={(event) => setShowOnlyInStock(event.target.checked)}
              />
              仅看有货
            </label>
            <select
              className="sku-filter-sort"
              value={skuSortBy}
              onChange={(event) => setSkuSortBy(event.target.value as 'default' | 'dropDesc' | 'finalAsc')}
            >
              <option value="default">默认顺序</option>
              <option value="dropDesc">按降价幅度</option>
              <option value="finalAsc">按到手价升序</option>
            </select>
            <input
              className="sku-filter-search"
              type="text"
              value={skuKeyword}
              onChange={(event) => setSkuKeyword(event.target.value)}
              placeholder="按商品名 / SKU / 店铺筛选"
            />
          </div>
          <div className="log-list">
            {skuList.length === 0 && (
              <div className="log-empty">暂无 SKU。命中 extract 规则的京东购物车响应会解析并显示在这里。</div>
            )}
            {skuList.length > 0 && filteredSkuList.length === 0 && (
              <div className="log-empty">当前筛选条件下没有结果，请放宽条件后再试。</div>
            )}
            {filteredSkuList.map((entry) => (
              <div className="log-card" key={entry.itemId}>
                <div className="log-card-top">
                  <span className="log-tag log-tag-action">{formatYuan(entry.finalPriceCents)}</span>
                  {entry.discountCents > 0 && (
                    <span className="log-tag log-tag-method">省{formatYuan(entry.discountCents)}</span>
                  )}
                  {entry.priceChanged && (
                    <span className={`log-tag ${entry.finalDeltaCents < 0 ? 'log-tag-status' : 'log-tag-method'}`}>
                      {entry.finalDeltaCents < 0 ? '降' : '涨'}{formatYuan(Math.abs(entry.finalDeltaCents))}
                    </span>
                  )}
                  {entry.stockDesc && <span className="log-tag log-tag-status">{entry.stockDesc}</span>}
                  <span className="log-time">×{entry.updateCount}</span>
                </div>
                <div className="log-card-bottom">
                  <span className="log-rule">{entry.vendorName || entry.vendorId || '未知店铺'}</span>
                  <span className="log-url" title={entry.name}>{entry.name || entry.itemId}</span>
                </div>
              </div>
            ))}
          </div>
          <p className="hint">
            价格为「到手价」，含 landedPrice 时按其计算，否则回退到页面价。降/涨标签对比上一次抓取到的到手价。
          </p>
        </Panel>

        <Panel title="证书与运行时" accent="gold" full>
          <div className="metric-list">
            <Metric label="证书信任" value={status.rootTrusted ? '已信任' : '未信任'} tone={status.rootTrusted ? 'good' : 'warn'} />
            <Metric label="证书指纹" value={status.rootThumbprint || '待生成'} compact />
            <Metric label="证书路径" value={status.rootCertPath || '待生成'} compact />
            <Metric label="应用数据目录" value={status.baseDir || '待生成'} compact />
            <Metric label="日志目录" value={status.logDir || '待生成'} compact />
            <Metric label="默认值" value={`${defaults.proxyAddr} · ${defaults.rulesPath}`} compact />
          </div>
          <div className="button-row">
            <button type="button" onClick={installCert} disabled={busy}>重新安装证书</button>
            <button type="button" onClick={uninstallCert} disabled={busy}>卸载证书</button>
          </div>
          {(notice || status.lastError) && <div className="notice">{notice || status.lastError}</div>}
        </Panel>
      </section>
    </main>
  );
}

function Panel({ title, accent, wide, full, children }: { title: string; accent: string; wide?: boolean; full?: boolean; children: React.ReactNode }) {
  return (
    <section className={`panel ${wide ? 'wide' : ''} ${full ? 'full' : ''}`} data-accent={accent}>
      <div className="panel-heading">
        <span />
        <h2>{title}</h2>
      </div>
      {children}
    </section>
  );
}

function Metric({ label, value, tone, compact }: { label: string; value: string; tone?: 'good' | 'warn'; compact?: boolean }) {
  return (
    <div className={`metric ${tone || ''} ${compact ? 'compact' : ''}`}>
      <span>{label}</span>
      <strong title={value}>{value}</strong>
    </div>
  );
}

function formatYuan(cents: number): string {
  return `¥${(cents / 100).toFixed(2)}`;
}

createRoot(document.getElementById('root')!).render(<App />);
