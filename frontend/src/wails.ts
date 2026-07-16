export type Status = {
  proxyRunning: boolean;
  addr: string;
  rulesPath: string;
  systemProxyActive: boolean;
  rootCertPath: string;
  rootThumbprint: string;
  rootTrusted: boolean;
  baseDir: string;
  logDir: string;
  proxyStatePath: string;
  licensed: boolean;
  lastError: string;
};

export type DesktopDefaults = {
  rulesPath: string;
  automationPath: string;
  proxyAddr: string;
  proxyOverride: string;
};

export type StartOptions = {
  addr: string;
  rulesPath: string;
  enableSystemProxy: boolean;
  proxyOverride: string;
};

export type JDAutomationOptions = {
  processName: string;
  windowTitleContains: string;
  inputMode: 'foreground' | 'background';
  repeatCount: number;
  cartTabXRatio: number;
  cartTabYRatio: number;
  allTabXRatio: number;
  allTabYRatio: number;
  serviceTabXRatio: number;
  serviceTabYRatio: number;
  firstDelaySeconds: number;
};

export type JDAutomationStatus = {
  running: boolean;
  currentCycle: number;
  totalCycles: number;
  lastError: string;
};

export type RequestLogEntry = {
  time: string;
  method: string;
  url: string;
  ruleName?: string;
  actionType?: string;
  status?: number;
};

export type SKUEntry = {
  itemId: string;
  name: string;
  vendorId: string;
  vendorName: string;
  pagePriceCents: number;
  finalPriceCents: number;
  discountCents: number;
  num: number;
  stockCode: number;
  stockDesc: string;
  remainNum: number;
  firstSeen: string;
  lastUpdated: string;
  updateCount: number;
  prevFinalCents: number;
  finalDeltaCents: number;
  priceChanged: boolean;
  stockChanged: boolean;
  promoChanged: boolean;
  giftChanged: boolean;
  changes?: Array<{
    category: 'price' | 'stock' | 'promo' | 'gift' | string;
    field: string;
    old: string;
    new: string;
    description?: string;
    oldNumber?: number;
    newNumber?: number;
    numeric?: boolean;
  }>;
};

export type SKUSnapshot = {
  entries: SKUEntry[] | null;
  updatedAt: string;
  parseCount: number;
  totalSku: number;
};

export type NotifyDingTalkConfig = {
  enabled?: boolean;
  webhookUrl: string;
  secret?: string;
};

export type NotifyBarkConfig = {
  enabled: boolean;
  serverUrl: string;
  deviceKey: string;
};

export type NotifyCategoryConfig = {
  price: boolean;
  stock: boolean;
  promo: boolean;
  gift: boolean;
};

export type NotifyConfig = {
  enabled: boolean;
  dingtalk: NotifyDingTalkConfig;
  bark: NotifyBarkConfig;
  categories: NotifyCategoryConfig;
  stockChangeThreshold: number;
  showProductUrl: boolean;
  showCheckoutUrl: boolean;
  showAppQrCode: boolean;
  quoteDiffFilterEnabled: boolean;
  quoteDiffThreshold: number;
  discountRate: number;
  format: string;
  title: string;
  template: string;
};

export type LicenseState = {
  key: string;
  deviceId: string;
  status: string;
  expiresAt: string;
  issuedAt: string;
  serverTime: string;
  nonce: string;
  signature: string;
  lastVerifiedAt: string;
};

type DesktopApi = {
  GetDefaults(): Promise<DesktopDefaults>;
  GetStatus(): Promise<Status>;
  StartProxy(options: StartOptions): Promise<Status>;
  StopProxy(): Promise<Status>;
  InstallCert(): Promise<Status>;
  UninstallCert(): Promise<Status>;
  StartJDAutomation(options: JDAutomationOptions): Promise<JDAutomationStatus>;
  StopJDAutomation(): Promise<JDAutomationStatus>;
  GetJDAutomationStatus(): Promise<JDAutomationStatus>;
  GetRequestLogs(): Promise<RequestLogEntry[]>;
  GetSKUList(): Promise<SKUSnapshot>;
  ResetSKUList(): Promise<SKUSnapshot>;
  GetNotifyConfig(): Promise<NotifyConfig>;
  SaveNotifyConfig(config: NotifyConfig): Promise<void>;
  TestNotify(config: NotifyConfig): Promise<void>;
  ActivateLicense(licenseKey: string): Promise<void>;
  VerifyLicense(): Promise<boolean>;
  DeactivateLicense(): Promise<void>;
  GetLicenseState(): Promise<LicenseState>;
  GetDeviceID(): Promise<string>;
  GetLicenseServerURL(): Promise<string>;
  SetLicenseServerURL(url: string): Promise<void>;
};

declare global {
  interface Window {
    go?: {
      main?: {
        DesktopApp?: DesktopApi;
      };
    };
  }
}

const desktopApp = window.go?.main?.DesktopApp;

export function api(): DesktopApi {
  if (!desktopApp) {
    throw new Error('Wails backend is not available. Run this screen with wails dev or the packaged desktop app.');
  }
  return desktopApp;
}
