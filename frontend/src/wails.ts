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

export type RulesValidationResult = {
  valid: boolean;
  count: number;
  error: string;
};

export type JDAutomationOptions = {
  processName: string;
  windowTitleContains: string;
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
  firstSeen: string;
  lastUpdated: string;
  updateCount: number;
  prevFinalCents: number;
  finalDeltaCents: number;
  priceChanged: boolean;
};

export type SKUSnapshot = {
  entries: SKUEntry[] | null;
  updatedAt: string;
  parseCount: number;
  totalSku: number;
};

type DesktopApi = {
  GetDefaults(): Promise<DesktopDefaults>;
  GetStatus(): Promise<Status>;
  StartProxy(options: StartOptions): Promise<Status>;
  StopProxy(): Promise<Status>;
  InstallCert(): Promise<Status>;
  UninstallCert(): Promise<Status>;
  ReadTextFile(path: string): Promise<string>;
  WriteTextFile(path: string, content: string): Promise<void>;
  ValidateRulesText(content: string): Promise<RulesValidationResult>;
  FormatJSON(content: string): Promise<string>;
  SelectJSONFile(title: string): Promise<string>;
  InspectAutomation(path: string): Promise<string>;
  RunAutomation(path: string): Promise<Status>;
  StartJDAutomation(options: JDAutomationOptions): Promise<JDAutomationStatus>;
  StopJDAutomation(): Promise<JDAutomationStatus>;
  GetJDAutomationStatus(): Promise<JDAutomationStatus>;
  GetRequestLogs(): Promise<RequestLogEntry[]>;
  GetSKUList(): Promise<SKUSnapshot>;
  ResetSKUList(): Promise<SKUSnapshot>;
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
