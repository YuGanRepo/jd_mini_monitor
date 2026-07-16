export namespace app {
	
	export class JDAutomationStatus {
	    running: boolean;
	    currentCycle: number;
	    totalCycles: number;
	    lastError: string;
	
	    static createFrom(source: any = {}) {
	        return new JDAutomationStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.currentCycle = source["currentCycle"];
	        this.totalCycles = source["totalCycles"];
	        this.lastError = source["lastError"];
	    }
	}
	export class Status {
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
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proxyRunning = source["proxyRunning"];
	        this.addr = source["addr"];
	        this.rulesPath = source["rulesPath"];
	        this.systemProxyActive = source["systemProxyActive"];
	        this.rootCertPath = source["rootCertPath"];
	        this.rootThumbprint = source["rootThumbprint"];
	        this.rootTrusted = source["rootTrusted"];
	        this.baseDir = source["baseDir"];
	        this.logDir = source["logDir"];
	        this.proxyStatePath = source["proxyStatePath"];
	        this.licensed = source["licensed"];
	        this.lastError = source["lastError"];
	    }
	}

}

export namespace license {
	
	export class State {
	    key: string;
	    deviceId: string;
	    status: string;
	    expiresAt: string;
	    issuedAt: string;
	    serverTime: string;
	    nonce: string;
	    signature: string;
	    lastVerifiedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.deviceId = source["deviceId"];
	        this.status = source["status"];
	        this.expiresAt = source["expiresAt"];
	        this.issuedAt = source["issuedAt"];
	        this.serverTime = source["serverTime"];
	        this.nonce = source["nonce"];
	        this.signature = source["signature"];
	        this.lastVerifiedAt = source["lastVerifiedAt"];
	    }
	}

}

export namespace main {
	
	export class DesktopDefaults {
	    rulesPath: string;
	    automationPath: string;
	    proxyAddr: string;
	    proxyOverride: string;
	
	    static createFrom(source: any = {}) {
	        return new DesktopDefaults(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rulesPath = source["rulesPath"];
	        this.automationPath = source["automationPath"];
	        this.proxyAddr = source["proxyAddr"];
	        this.proxyOverride = source["proxyOverride"];
	    }
	}
	export class DesktopStartOptions {
	    addr: string;
	    rulesPath: string;
	    enableSystemProxy: boolean;
	    proxyOverride: string;
	
	    static createFrom(source: any = {}) {
	        return new DesktopStartOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.addr = source["addr"];
	        this.rulesPath = source["rulesPath"];
	        this.enableSystemProxy = source["enableSystemProxy"];
	        this.proxyOverride = source["proxyOverride"];
	    }
	}

}

export namespace notify {
	
	export class BarkConfig {
	    enabled: boolean;
	    serverUrl: string;
	    deviceKey: string;
	
	    static createFrom(source: any = {}) {
	        return new BarkConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.serverUrl = source["serverUrl"];
	        this.deviceKey = source["deviceKey"];
	    }
	}
	export class CategoryConfig {
	    price: boolean;
	    stock: boolean;
	    promo: boolean;
	    gift: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CategoryConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.price = source["price"];
	        this.stock = source["stock"];
	        this.promo = source["promo"];
	        this.gift = source["gift"];
	    }
	}
	export class DingTalkConfig {
	    enabled?: boolean;
	    webhookUrl: string;
	    secret?: string;
	
	    static createFrom(source: any = {}) {
	        return new DingTalkConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.webhookUrl = source["webhookUrl"];
	        this.secret = source["secret"];
	    }
	}
	export class Config {
	    enabled: boolean;
	    dingtalk: DingTalkConfig;
	    bark: BarkConfig;
	    categories?: CategoryConfig;
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
	
	    static createFrom(source: any = {}) {
	        return new Config(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.dingtalk = this.convertValues(source["dingtalk"], DingTalkConfig);
	        this.bark = this.convertValues(source["bark"], BarkConfig);
	        this.categories = this.convertValues(source["categories"], CategoryConfig);
	        this.stockChangeThreshold = source["stockChangeThreshold"];
	        this.showProductUrl = source["showProductUrl"];
	        this.showCheckoutUrl = source["showCheckoutUrl"];
	        this.showAppQrCode = source["showAppQrCode"];
	        this.quoteDiffFilterEnabled = source["quoteDiffFilterEnabled"];
	        this.quoteDiffThreshold = source["quoteDiffThreshold"];
	        this.discountRate = source["discountRate"];
	        this.format = source["format"];
	        this.title = source["title"];
	        this.template = source["template"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace proxy {
	
	export class RequestLogEntry {
	    // Go type: time
	    time: any;
	    method: string;
	    url: string;
	    ruleName?: string;
	    actionType?: string;
	    status?: number;
	
	    static createFrom(source: any = {}) {
	        return new RequestLogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = this.convertValues(source["time"], null);
	        this.method = source["method"];
	        this.url = source["url"];
	        this.ruleName = source["ruleName"];
	        this.actionType = source["actionType"];
	        this.status = source["status"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace sku {
	
	export class Change {
	    category: string;
	    field: string;
	    old: string;
	    new: string;
	    description?: string;
	    oldNumber?: number;
	    newNumber?: number;
	    numeric?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Change(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.category = source["category"];
	        this.field = source["field"];
	        this.old = source["old"];
	        this.new = source["new"];
	        this.description = source["description"];
	        this.oldNumber = source["oldNumber"];
	        this.newNumber = source["newNumber"];
	        this.numeric = source["numeric"];
	    }
	}
	export class Gift {
	    id: string;
	    name: string;
	    num: number;
	
	    static createFrom(source: any = {}) {
	        return new Gift(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.num = source["num"];
	    }
	}
	export class Promo {
	    id: string;
	    title: string;
	    type?: string;
	
	    static createFrom(source: any = {}) {
	        return new Promo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.type = source["type"];
	    }
	}
	export class Entry {
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
	    pricePrim?: string;
	    priceShow?: string;
	    priceDescription?: string;
	    priceRevert?: string;
	    plusDiscount?: string;
	    plusText?: string;
	    selectedPromos?: Promo[];
	    cutPriceText?: string;
	    cutPriceCents?: number;
	    gifts?: Gift[];
	    subsidyText?: string;
	    productUrl?: string;
	    checkoutUrl?: string;
	    // Go type: time
	    firstSeen: any;
	    // Go type: time
	    lastUpdated: any;
	    updateCount: number;
	    prevFinalCents: number;
	    finalDeltaCents: number;
	    priceChanged: boolean;
	    stockChanged: boolean;
	    promoChanged: boolean;
	    giftChanged: boolean;
	    changes?: Change[];
	
	    static createFrom(source: any = {}) {
	        return new Entry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.itemId = source["itemId"];
	        this.name = source["name"];
	        this.vendorId = source["vendorId"];
	        this.vendorName = source["vendorName"];
	        this.pagePriceCents = source["pagePriceCents"];
	        this.finalPriceCents = source["finalPriceCents"];
	        this.discountCents = source["discountCents"];
	        this.num = source["num"];
	        this.stockCode = source["stockCode"];
	        this.stockDesc = source["stockDesc"];
	        this.remainNum = source["remainNum"];
	        this.pricePrim = source["pricePrim"];
	        this.priceShow = source["priceShow"];
	        this.priceDescription = source["priceDescription"];
	        this.priceRevert = source["priceRevert"];
	        this.plusDiscount = source["plusDiscount"];
	        this.plusText = source["plusText"];
	        this.selectedPromos = this.convertValues(source["selectedPromos"], Promo);
	        this.cutPriceText = source["cutPriceText"];
	        this.cutPriceCents = source["cutPriceCents"];
	        this.gifts = this.convertValues(source["gifts"], Gift);
	        this.subsidyText = source["subsidyText"];
	        this.productUrl = source["productUrl"];
	        this.checkoutUrl = source["checkoutUrl"];
	        this.firstSeen = this.convertValues(source["firstSeen"], null);
	        this.lastUpdated = this.convertValues(source["lastUpdated"], null);
	        this.updateCount = source["updateCount"];
	        this.prevFinalCents = source["prevFinalCents"];
	        this.finalDeltaCents = source["finalDeltaCents"];
	        this.priceChanged = source["priceChanged"];
	        this.stockChanged = source["stockChanged"];
	        this.promoChanged = source["promoChanged"];
	        this.giftChanged = source["giftChanged"];
	        this.changes = this.convertValues(source["changes"], Change);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snapshot {
	    entries: Entry[];
	    // Go type: time
	    updatedAt: any;
	    parseCount: number;
	    totalSku: number;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entries = this.convertValues(source["entries"], Entry);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.parseCount = source["parseCount"];
	        this.totalSku = source["totalSku"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace uiauto {
	
	export class CoordCycleOptions {
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
	
	    static createFrom(source: any = {}) {
	        return new CoordCycleOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.processName = source["processName"];
	        this.windowTitleContains = source["windowTitleContains"];
	        this.repeatCount = source["repeatCount"];
	        this.cartTabXRatio = source["cartTabXRatio"];
	        this.cartTabYRatio = source["cartTabYRatio"];
	        this.allTabXRatio = source["allTabXRatio"];
	        this.allTabYRatio = source["allTabYRatio"];
	        this.serviceTabXRatio = source["serviceTabXRatio"];
	        this.serviceTabYRatio = source["serviceTabYRatio"];
	        this.firstDelaySeconds = source["firstDelaySeconds"];
	    }
	}

}

