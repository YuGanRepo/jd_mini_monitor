import React, { lazy, Suspense, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import Alert from 'antd/es/alert';
import AntApp from 'antd/es/app';
import Button from 'antd/es/button';
import Card from 'antd/es/card';
import ConfigProvider from 'antd/es/config-provider';
import Input from 'antd/es/input';
import Space from 'antd/es/space';
import Spin from 'antd/es/spin';
import Typography from 'antd/es/typography';
import { SafetyCertificateOutlined } from '@ant-design/icons';
import zhCN from 'antd/locale/zh_CN';
import { ACCENT, THEME } from './theme';
import { api, type LicenseState } from './wails';
import './styles.css';

const Dashboard = lazy(() => import('./Dashboard'));

const emptyLicenseState: LicenseState = {
  key: '',
  deviceId: '',
  status: '',
  expiresAt: '',
  issuedAt: '',
  serverTime: '',
  nonce: '',
  signature: '',
  lastVerifiedAt: '',
};

const REASON_TEXT: Record<string, string> = {
  network: '无法连接授权服务器，请检查网络或服务器地址后重试。',
  网络请求失败: '无法连接授权服务器，请检查网络或服务器地址后重试。',
  expired: '授权已过期，请续费或联系管理员延期。',
  revoked: '该授权码已被吊销，请联系管理员。',
  'device-mismatch': '该授权码已绑定其他设备。如需换机，请联系管理员解绑后重新激活。',
  'device-limit': '该授权码绑定的设备数已达上限。',
  'license-not-found': '授权码不存在，请核对后重试。',
  'invalid-signature': '授权校验失败（签名无效），请联系管理员。',
  'invalid-response': '授权服务器返回异常，请稍后重试。',
  'key-mismatch': '授权校验失败（授权码不匹配）。',
};

function LicenseShell() {
  const [licenseState, setLicenseState] = useState<LicenseState>(emptyLicenseState);
  const [licenseKey, setLicenseKey] = useState('');
  const [deviceID, setDeviceID] = useState('');
  const [licenseServerURL, setLicenseServerURL] = useState('');
  const [lastError, setLastError] = useState('');
  const [licenseChecked, setLicenseChecked] = useState(false);
  const [licenseAuthorized, setLicenseAuthorized] = useState(false);
  const [busy, setBusy] = useState(false);
  const { message } = AntApp.useApp();

  useEffect(() => {
    void checkLicense();
  }, []);

  async function checkLicense() {
    try {
      const [id, serverURL] = await Promise.all([
        api().GetDeviceID(),
        api().GetLicenseServerURL(),
      ]);
      setDeviceID(id);
      setLicenseServerURL(serverURL);

      let authorized = false;
      try {
        authorized = await api().VerifyLicense();
      } catch {
        authorized = (await api().GetStatus()).licensed;
      }
      const [state, status] = await Promise.all([
        api().GetLicenseState(),
        api().GetStatus(),
      ]);
      setLicenseState(state);
      setLastError(status.lastError);
      setLicenseAuthorized(authorized);
    } catch {
      // Keep compatibility with an older desktop backend that has no license API.
      setLicenseAuthorized(true);
    } finally {
      setLicenseChecked(true);
    }
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

  async function activateLicense() {
    await runTask(async () => {
      await api().ActivateLicense(licenseKey);
      await checkLicense();
      setLicenseKey('');
    }, '授权码已激活');
  }

  async function deactivateLicense() {
    await runTask(async () => {
      await api().DeactivateLicense();
      setLicenseState(emptyLicenseState);
      setLicenseAuthorized(false);
      setLastError('');
    }, '已停用授权码');
  }

  async function saveLicenseServer() {
    await runTask(async () => {
      await api().SetLicenseServerURL(licenseServerURL);
      await checkLicense();
    }, '授权服务器地址已保存');
  }

  if (!licenseChecked) {
    return <CenteredLoading tip="正在校验授权…" />;
  }

  if (licenseAuthorized) {
    return (
      <Suspense fallback={<CenteredLoading tip="正在加载控制台…" />}>
        <Dashboard
          licenseState={licenseState}
          licenseBusy={busy}
          onDeactivateLicense={deactivateLicense}
        />
      </Suspense>
    );
  }

  return (
    <div className="app-shell" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '100vh' }}>
      <Card style={{ width: 480, textAlign: 'center' }} bordered>
        <Space direction="vertical" size="large" style={{ width: '100%' }}>
          <div>
            <SafetyCertificateOutlined style={{ fontSize: 40, color: ACCENT.mint }} />
            <Typography.Title level={3} style={{ marginTop: 12, marginBottom: 4 }}>授权验证</Typography.Title>
            <Typography.Text type="secondary">Mini Proxy · License 激活（联网校验）</Typography.Text>
          </div>

          {lastError && (
            <Alert
              type={lastError.includes('网络') ? 'warning' : 'error'}
              message={licenseReasonText(lastError)}
              showIcon
            />
          )}

          <div style={{ textAlign: 'left' }}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>本机设备 ID（换机解绑时提供给管理员）</Typography.Text>
            <Typography.Paragraph copyable={{ text: deviceID || '' }} style={{ marginBottom: 0, marginTop: 4 }}>
              <Typography.Text code>{deviceID || '生成中...'}</Typography.Text>
            </Typography.Paragraph>
          </div>

          <div style={{ textAlign: 'left' }}>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>授权码</Typography.Text>
            <Input
              value={licenseKey}
              onChange={(event) => setLicenseKey(event.target.value)}
              onPressEnter={() => void activateLicense()}
              placeholder="XXXX-XXXX-XXXX-XXXX"
              autoFocus
              style={{ marginTop: 4, fontFamily: '"Cascadia Code", Consolas, monospace', letterSpacing: 1 }}
            />
          </div>

          <details style={{ textAlign: 'left' }}>
            <summary style={{ cursor: 'pointer', color: '#888', fontSize: 12 }}>授权服务器地址（高级）</summary>
            <Space.Compact style={{ width: '100%', marginTop: 8 }}>
              <Input
                value={licenseServerURL}
                onChange={(event) => setLicenseServerURL(event.target.value)}
                placeholder="http://118.196.100.19:8787"
                style={{ fontFamily: '"Cascadia Code", Consolas, monospace' }}
              />
              <Button onClick={() => void saveLicenseServer()} loading={busy}>保存</Button>
            </Space.Compact>
          </details>

          <Space style={{ width: '100%', justifyContent: 'space-between' }}>
            <Button onClick={() => void runTask(checkLicense)} loading={busy}>重新校验</Button>
            <Button type="primary" onClick={() => void activateLicense()} loading={busy}>激活</Button>
          </Space>

          <Typography.Text type="secondary" style={{ fontSize: 12, display: 'block' }}>
            授权码由管理员在授权服务器签发。激活需联网校验并绑定本机设备。
          </Typography.Text>
        </Space>
      </Card>
    </div>
  );
}

function CenteredLoading({ tip }: { tip: string }) {
  return (
    <div className="app-shell" style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '100vh' }}>
      <Spin size="large" tip={tip} />
    </div>
  );
}

function licenseReasonText(lastError: string): string {
  if (!lastError) return '请输入授权码以激活。';
  for (const [key, text] of Object.entries(REASON_TEXT)) {
    if (lastError.includes(key)) return text;
  }
  return `授权校验未通过：${lastError}`;
}

function Root() {
  return (
    <ConfigProvider locale={zhCN} theme={THEME}>
      <AntApp>
        <LicenseShell />
      </AntApp>
    </ConfigProvider>
  );
}

createRoot(document.getElementById('root')!).render(<Root />);
