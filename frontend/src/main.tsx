import React, { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { api, DesktopDefaults, JDAutomationOptions, JDAutomationStatus, RequestLogEntry, Status } from './wails';
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

createRoot(document.getElementById('root')!).render(<App />);
