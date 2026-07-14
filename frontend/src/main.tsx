import React, { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { api, DesktopDefaults, JDAutomationOptions, JDAutomationStatus, RulesValidationResult, Status } from './wails';
import './styles.css';

const emptyStatus: Status = {
  proxyRunning: false,
  addr: '127.0.0.1:8899',
  rulesPath: 'configs/example.rules.json',
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
    rulesPath: 'configs/example.rules.json',
    automationPath: 'configs/example.automation.json',
    proxyAddr: '127.0.0.1:8899',
    proxyOverride: 'localhost;127.0.0.1;<local>',
  });
  const [addr, setAddr] = useState('127.0.0.1:8899');
  const [rulesPath, setRulesPath] = useState('configs/example.rules.json');
  const [automationPath, setAutomationPath] = useState('configs/example.automation.json');
  const [proxyOverride, setProxyOverride] = useState('localhost;127.0.0.1;<local>');
  const [enableSystemProxy, setEnableSystemProxy] = useState(true);
  const [rulesText, setRulesText] = useState('');
  const [validation, setValidation] = useState<RulesValidationResult>({ valid: false, count: 0, error: 'Not validated yet' });
  const [automationOutput, setAutomationOutput] = useState('');
  const [jdOptions, setJdOptions] = useState<JDAutomationOptions>(defaultJDOptions);
  const [jdStatus, setJdStatus] = useState<JDAutomationStatus>(emptyJDStatus);
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    void initialize();
  }, []);

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
      setAutomationPath(loadedDefaults.automationPath);
      setProxyOverride(loadedDefaults.proxyOverride);
      const nextStatus = await api().GetStatus();
      setStatus(nextStatus);
      await loadRules(loadedDefaults.rulesPath);
    }, 'Ready');
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

  async function refreshStatus() {
    const nextStatus = await api().GetStatus();
    setStatus(nextStatus);
  }

  async function loadRules(path = rulesPath) {
    const content = await api().ReadTextFile(path);
    setRulesText(content);
    setRulesPath(path);
    setValidation(await api().ValidateRulesText(content));
  }

  async function chooseRulesFile() {
    await runTask(async () => {
      const selected = await api().SelectJSONFile('Select rules JSON');
      if (selected) await loadRules(selected);
    });
  }

  async function saveRules() {
    await runTask(async () => {
      const result = await api().ValidateRulesText(rulesText);
      setValidation(result);
      if (!result.valid) throw new Error(result.error);
      await api().WriteTextFile(rulesPath, rulesText);
      await refreshStatus();
    }, 'Rules saved');
  }

  async function formatRules() {
    await runTask(async () => {
      const formatted = await api().FormatJSON(rulesText);
      setRulesText(formatted);
      setValidation(await api().ValidateRulesText(formatted));
    }, 'Rules formatted');
  }

  async function startProxy() {
    await runTask(async () => {
      const result = await api().ValidateRulesText(rulesText);
      setValidation(result);
      if (!result.valid) throw new Error(result.error);
      await api().WriteTextFile(rulesPath, rulesText);
      setStatus(await api().StartProxy({ addr, rulesPath, enableSystemProxy, proxyOverride }));
    }, 'Proxy started');
  }

  async function stopProxy() {
    await runTask(async () => {
      setStatus(await api().StopProxy());
    }, 'Proxy stopped');
  }

  async function installCert() {
    await runTask(async () => {
      setStatus(await api().InstallCert());
    }, 'Certificate installed');
  }

  async function uninstallCert() {
    await runTask(async () => {
      setStatus(await api().UninstallCert());
    }, 'Certificate uninstalled');
  }

  async function chooseAutomationFile() {
    await runTask(async () => {
      const selected = await api().SelectJSONFile('Select automation JSON');
      if (selected) setAutomationPath(selected);
    });
  }

  async function inspectAutomation() {
    await runTask(async () => {
      const output = await api().InspectAutomation(automationPath);
      setAutomationOutput(output.trim() || 'No buttons matched the current window selector.');
      await refreshStatus();
    }, 'Inspection finished');
  }

  async function runAutomation() {
    await runTask(async () => {
      setStatus(await api().RunAutomation(automationPath));
      setAutomationOutput('Automation sequence completed. Check logs for per-step click details.');
    }, 'Automation finished');
  }

  async function toggleJDAutomation(enabled: boolean) {
    await runTask(async () => {
      if (enabled) {
        setJdStatus(await api().StartJDAutomation(jdOptions));
      } else {
        setJdStatus(await api().StopJDAutomation());
      }
    }, enabled ? 'JD automation started' : 'JD automation stopped');
  }

  return (
    <main className="shell">
      <section className="topbar">
        <div>
          <p className="eyebrow">Mini Proxy Desktop</p>
          <h1>HTTPS Interception And Window Automation</h1>
        </div>
        <div className={`status-pill ${status.proxyRunning ? 'running' : ''}`}>
          <span />
          {status.proxyRunning ? 'Proxy running' : 'Proxy stopped'}
        </div>
      </section>

      <section className="dashboard-grid">
        <Panel title="Proxy Control" accent="mint">
          <div className="field-row two">
            <label>
              Listen address
              <input value={addr} onChange={(event) => setAddr(event.target.value)} disabled={status.proxyRunning || busy} />
            </label>
            <label>
              Rules file
              <div className="input-button">
                <input value={rulesPath} onChange={(event) => setRulesPath(event.target.value)} disabled={status.proxyRunning || busy} />
                <button type="button" onClick={chooseRulesFile} disabled={status.proxyRunning || busy}>Browse</button>
              </div>
            </label>
          </div>
          <label>
            Proxy bypass list
            <input value={proxyOverride} onChange={(event) => setProxyOverride(event.target.value)} disabled={status.proxyRunning || busy} />
          </label>
          <label className="toggle-line">
            <input type="checkbox" checked={enableSystemProxy} onChange={(event) => setEnableSystemProxy(event.target.checked)} disabled={status.proxyRunning || busy} />
            Set Windows system proxy while running
          </label>
          <div className="button-row">
            <button type="button" className="primary" onClick={startProxy} disabled={status.proxyRunning || busy}>Start</button>
            <button type="button" onClick={stopProxy} disabled={!status.proxyRunning || busy}>Stop</button>
            <button type="button" onClick={() => void runTask(refreshStatus, 'Status refreshed')} disabled={busy}>Refresh</button>
          </div>
        </Panel>

        <Panel title="Certificate" accent="gold">
          <div className="metric-list">
            <Metric label="Trust" value={status.rootTrusted ? 'Trusted' : 'Not trusted'} tone={status.rootTrusted ? 'good' : 'warn'} />
            <Metric label="Thumbprint" value={status.rootThumbprint || 'Pending'} />
            <Metric label="Certificate" value={status.rootCertPath || 'Pending'} compact />
          </div>
          <div className="button-row">
            <button type="button" onClick={installCert} disabled={busy}>Install</button>
            <button type="button" onClick={uninstallCert} disabled={busy}>Uninstall</button>
          </div>
        </Panel>

        <Panel title="Rules Editor" accent="ink" wide>
          <div className="editor-toolbar">
            <span className={validation.valid ? 'validation good' : 'validation warn'}>
              {validation.valid ? `${validation.count} valid rule${validation.count === 1 ? '' : 's'}` : validation.error}
            </span>
            <div className="button-row compact">
              <button type="button" onClick={() => void runTask(() => loadRules(), 'Rules loaded')} disabled={busy}>Reload</button>
              <button type="button" onClick={formatRules} disabled={busy}>Format</button>
              <button type="button" onClick={saveRules} disabled={busy}>Save</button>
            </div>
          </div>
          <textarea className="rules-editor" value={rulesText} onChange={(event) => setRulesText(event.target.value)} spellCheck={false} />
        </Panel>

        <Panel title="Button Automation" accent="coral">
          <label>
            Automation file
            <div className="input-button">
              <input value={automationPath} onChange={(event) => setAutomationPath(event.target.value)} disabled={busy} />
              <button type="button" onClick={chooseAutomationFile} disabled={busy}>Browse</button>
            </div>
          </label>
          <div className="button-row">
            <button type="button" onClick={inspectAutomation} disabled={busy}>Inspect</button>
            <button type="button" className="primary warm" onClick={runAutomation} disabled={busy}>Run Sequence</button>
          </div>
          <pre className="automation-output">{automationOutput || 'Inspection results and click logs appear here.'}</pre>
        </Panel>

        <Panel title="JD Mini Program Automation" accent="coral">
          <div className="field-row two">
            <label>
              Window title contains
              <input
                value={jdOptions.windowTitleContains}
                onChange={(event) => setJdOptions({ ...jdOptions, windowTitleContains: event.target.value })}
                disabled={jdStatus.running || busy}
              />
            </label>
            <label>
              Host process
              <input
                value={jdOptions.processName}
                onChange={(event) => setJdOptions({ ...jdOptions, processName: event.target.value })}
                disabled={jdStatus.running || busy}
              />
            </label>
          </div>
          <div className="field-row two">
            <label>
              循环次数 (repeat count)
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
          <label className="toggle-line">
            <input
              type="checkbox"
              checked={jdStatus.running}
              onChange={(event) => void toggleJDAutomation(event.target.checked)}
              disabled={busy}
            />
            开启自动化循环运行
          </label>
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

        <Panel title="Runtime Paths" accent="slate">
          <div className="metric-list">
            <Metric label="App data" value={status.baseDir || 'Pending'} compact />
            <Metric label="Logs" value={status.logDir || 'Pending'} compact />
            <Metric label="Saved proxy state" value={status.proxyStatePath || 'Pending'} compact />
            <Metric label="Defaults" value={`${defaults.proxyAddr} · ${defaults.rulesPath}`} compact />
          </div>
          {(notice || status.lastError) && <div className="notice">{notice || status.lastError}</div>}
        </Panel>
      </section>
    </main>
  );
}

function Panel({ title, accent, wide, children }: { title: string; accent: string; wide?: boolean; children: React.ReactNode }) {
  return (
    <section className={`panel ${wide ? 'wide' : ''}`} data-accent={accent}>
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
