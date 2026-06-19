export namespace adb {
	
	export class App {
	    pkg: string;
	    path: string;
	    system: boolean;
	    name: string;
	    v: string;
	    uid: string;
	
	    static createFrom(source: any = {}) {
	        return new App(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pkg = source["pkg"];
	        this.path = source["path"];
	        this.system = source["system"];
	        this.name = source["name"];
	        this.v = source["v"];
	        this.uid = source["uid"];
	    }
	}
	export class GrantedPerm {
	    name: string;
	    granted: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GrantedPerm(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.granted = source["granted"];
	    }
	}
	export class AppDetail {
	    pkg: string;
	    path: string;
	    system: boolean;
	    name: string;
	    v: string;
	    uid: string;
	    versionCode: string;
	    firstInstall: string;
	    lastUpdate: string;
	    timeStamp: string;
	    targetSdk: string;
	    minSdk: string;
	    compileSdk: string;
	    dataDir: string;
	    nativeLibraryDir: string;
	    installer: string;
	    installLocation: string;
	    primaryAbi: string;
	    secondaryAbi: string;
	    splits: string[];
	    flags: string[];
	    privateFlags: string[];
	    supportsScreens: string[];
	    signature: string;
	    apkSigningVersion: string;
	    enabled: string;
	    installed: string;
	    stopped: string;
	    notLaunched: string;
	    suspended: string;
	    instant: string;
	    gids: string[];
	    requestedPerms: string[];
	    grantedPerms: GrantedPerm[];
	
	    static createFrom(source: any = {}) {
	        return new AppDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pkg = source["pkg"];
	        this.path = source["path"];
	        this.system = source["system"];
	        this.name = source["name"];
	        this.v = source["v"];
	        this.uid = source["uid"];
	        this.versionCode = source["versionCode"];
	        this.firstInstall = source["firstInstall"];
	        this.lastUpdate = source["lastUpdate"];
	        this.timeStamp = source["timeStamp"];
	        this.targetSdk = source["targetSdk"];
	        this.minSdk = source["minSdk"];
	        this.compileSdk = source["compileSdk"];
	        this.dataDir = source["dataDir"];
	        this.nativeLibraryDir = source["nativeLibraryDir"];
	        this.installer = source["installer"];
	        this.installLocation = source["installLocation"];
	        this.primaryAbi = source["primaryAbi"];
	        this.secondaryAbi = source["secondaryAbi"];
	        this.splits = source["splits"];
	        this.flags = source["flags"];
	        this.privateFlags = source["privateFlags"];
	        this.supportsScreens = source["supportsScreens"];
	        this.signature = source["signature"];
	        this.apkSigningVersion = source["apkSigningVersion"];
	        this.enabled = source["enabled"];
	        this.installed = source["installed"];
	        this.stopped = source["stopped"];
	        this.notLaunched = source["notLaunched"];
	        this.suspended = source["suspended"];
	        this.instant = source["instant"];
	        this.gids = source["gids"];
	        this.requestedPerms = source["requestedPerms"];
	        this.grantedPerms = this.convertValues(source["grantedPerms"], GrantedPerm);
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
	export class AppRunning {
	    running: boolean;
	    pid: number;
	
	    static createFrom(source: any = {}) {
	        return new AppRunning(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.pid = source["pid"];
	    }
	}
	export class StepResult {
	    name: string;
	    status: string;
	    message: string;
	    needsReboot: boolean;
	
	    static createFrom(source: any = {}) {
	        return new StepResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.needsReboot = source["needsReboot"];
	    }
	}
	export class ApplyReport {
	    profileId: string;
	    profileName: string;
	    serial: string;
	    rooted: boolean;
	    steps: StepResult[];
	    needsReboot: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ApplyReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.profileId = source["profileId"];
	        this.profileName = source["profileName"];
	        this.serial = source["serial"];
	        this.rooted = source["rooted"];
	        this.steps = this.convertValues(source["steps"], StepResult);
	        this.needsReboot = source["needsReboot"];
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
	export class CACert {
	    fileName: string;
	    store: string;
	    subject: string;
	    issuer: string;
	    notAfter: string;
	    expired: boolean;
	    selfSigned: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CACert(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.store = source["store"];
	        this.subject = source["subject"];
	        this.issuer = source["issuer"];
	        this.notAfter = source["notAfter"];
	        this.expired = source["expired"];
	        this.selfSigned = source["selfSigned"];
	    }
	}
	export class CaptureState {
	    active: boolean;
	    ourSession: boolean;
	    startedAt: number;
	    pid: number;
	    remoteFile: string;
	    bpf: string;
	    iface: string;
	    sizeBytes: number;
	    packetHint: string;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new CaptureState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.ourSession = source["ourSession"];
	        this.startedAt = source["startedAt"];
	        this.pid = source["pid"];
	        this.remoteFile = source["remoteFile"];
	        this.bpf = source["bpf"];
	        this.iface = source["iface"];
	        this.sizeBytes = source["sizeBytes"];
	        this.packetHint = source["packetHint"];
	        this.warning = source["warning"];
	    }
	}
	export class CaptureStep {
	    enabled: boolean;
	    iface: string;
	    bpf: string;
	
	    static createFrom(source: any = {}) {
	        return new CaptureStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.iface = source["iface"];
	        this.bpf = source["bpf"];
	    }
	}
	export class CertInstallResult {
	    subject: string;
	    fileName: string;
	    path: string;
	    strategy: string;
	    persistent: boolean;
	    rooted: boolean;
	    sdk: number;
	    note: string;
	    diagnostics: string;
	
	    static createFrom(source: any = {}) {
	        return new CertInstallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.subject = source["subject"];
	        this.fileName = source["fileName"];
	        this.path = source["path"];
	        this.strategy = source["strategy"];
	        this.persistent = source["persistent"];
	        this.rooted = source["rooted"];
	        this.sdk = source["sdk"];
	        this.note = source["note"];
	        this.diagnostics = source["diagnostics"];
	    }
	}
	export class CertStep {
	    enabled: boolean;
	    pem: string;
	    subject: string;
	
	    static createFrom(source: any = {}) {
	        return new CertStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.pem = source["pem"];
	        this.subject = source["subject"];
	    }
	}
	export class Connection {
	    proto: string;
	    local: string;
	    remote: string;
	    state: string;
	    uid: number;
	    inode: string;
	
	    static createFrom(source: any = {}) {
	        return new Connection(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.proto = source["proto"];
	        this.local = source["local"];
	        this.remote = source["remote"];
	        this.state = source["state"];
	        this.uid = source["uid"];
	        this.inode = source["inode"];
	    }
	}
	export class Device {
	    id: string;
	    state: string;
	    online: boolean;
	    via: string;
	    transport: string;
	    label: string;
	    model: string;
	    product: string;
	    manufacturer: string;
	    androidVersion: string;
	    sdk: number;
	    buildId: string;
	    kernel: string;
	    cpu: string;
	    arch: string;
	    root: boolean;
	    rootMethod: string;
	    ip: string;
	    wifi: string;
	    mac: string;
	    hardwareSerial: string;
	
	    static createFrom(source: any = {}) {
	        return new Device(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.state = source["state"];
	        this.online = source["online"];
	        this.via = source["via"];
	        this.transport = source["transport"];
	        this.label = source["label"];
	        this.model = source["model"];
	        this.product = source["product"];
	        this.manufacturer = source["manufacturer"];
	        this.androidVersion = source["androidVersion"];
	        this.sdk = source["sdk"];
	        this.buildId = source["buildId"];
	        this.kernel = source["kernel"];
	        this.cpu = source["cpu"];
	        this.arch = source["arch"];
	        this.root = source["root"];
	        this.rootMethod = source["rootMethod"];
	        this.ip = source["ip"];
	        this.wifi = source["wifi"];
	        this.mac = source["mac"];
	        this.hardwareSerial = source["hardwareSerial"];
	    }
	}
	export class DeviceRecord {
	    key: string;
	    adbSerial: string;
	    hardwareSerial: string;
	    label: string;
	    model: string;
	    manufacturer: string;
	    firstSeen: number;
	    lastSeen: number;
	    boundProfileId: string;
	
	    static createFrom(source: any = {}) {
	        return new DeviceRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.adbSerial = source["adbSerial"];
	        this.hardwareSerial = source["hardwareSerial"];
	        this.label = source["label"];
	        this.model = source["model"];
	        this.manufacturer = source["manufacturer"];
	        this.firstSeen = source["firstSeen"];
	        this.lastSeen = source["lastSeen"];
	        this.boundProfileId = source["boundProfileId"];
	    }
	}
	export class FileEntry {
	    name: string;
	    type: string;
	    size: number;
	    perms: string;
	    owner: string;
	    group: string;
	    mtime: string;
	    link?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.size = source["size"];
	        this.perms = source["perms"];
	        this.owner = source["owner"];
	        this.group = source["group"];
	        this.mtime = source["mtime"];
	        this.link = source["link"];
	    }
	}
	export class Forward {
	    serial: string;
	    local: string;
	    remote: string;
	
	    static createFrom(source: any = {}) {
	        return new Forward(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serial = source["serial"];
	        this.local = source["local"];
	        this.remote = source["remote"];
	    }
	}
	export class ForwardSpec {
	    local: string;
	    remote: string;
	
	    static createFrom(source: any = {}) {
	        return new ForwardSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.local = source["local"];
	        this.remote = source["remote"];
	    }
	}
	export class ReverseSpec {
	    remote: string;
	    local: string;
	
	    static createFrom(source: any = {}) {
	        return new ReverseSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.remote = source["remote"];
	        this.local = source["local"];
	    }
	}
	export class ForwardsStep {
	    enabled: boolean;
	    forwards: ForwardSpec[];
	    reverses: ReverseSpec[];
	
	    static createFrom(source: any = {}) {
	        return new ForwardsStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.forwards = this.convertValues(source["forwards"], ForwardSpec);
	        this.reverses = this.convertValues(source["reverses"], ReverseSpec);
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
	export class FridaArchInfo {
	    abi: string;
	    abiList: string;
	    bits64: boolean;
	    primary: string;
	    supported: string[];
	
	    static createFrom(source: any = {}) {
	        return new FridaArchInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.abi = source["abi"];
	        this.abiList = source["abiList"];
	        this.bits64 = source["bits64"];
	        this.primary = source["primary"];
	        this.supported = source["supported"];
	    }
	}
	export class FridaRelease {
	    version: string;
	    arch: string;
	    assetURL: string;
	    size: number;
	    sha256: string;
	    installed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FridaRelease(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.arch = source["arch"];
	        this.assetURL = source["assetURL"];
	        this.size = source["size"];
	        this.sha256 = source["sha256"];
	        this.installed = source["installed"];
	    }
	}
	export class FridaServer {
	    name: string;
	    path: string;
	    version: string;
	    arch: string;
	    size: number;
	    perms: string;
	    active: boolean;
	    pid: number;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new FridaServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.arch = source["arch"];
	        this.size = source["size"];
	        this.perms = source["perms"];
	        this.active = source["active"];
	        this.pid = source["pid"];
	        this.port = source["port"];
	    }
	}
	export class FridaStep {
	    enabled: boolean;
	    version: string;
	    autoArch: boolean;
	    arch?: string;
	    start: boolean;
	    iface?: string;
	    port?: number;
	
	    static createFrom(source: any = {}) {
	        return new FridaStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.version = source["version"];
	        this.autoArch = source["autoArch"];
	        this.arch = source["arch"];
	        this.start = source["start"];
	        this.iface = source["iface"];
	        this.port = source["port"];
	    }
	}
	
	export class HostsApplyResult {
	    path: string;
	    strategy: string;
	    needsReboot: boolean;
	    netdFlushed: boolean;
	    content: string;
	    diagnostics: string;
	
	    static createFrom(source: any = {}) {
	        return new HostsApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.strategy = source["strategy"];
	        this.needsReboot = source["needsReboot"];
	        this.netdFlushed = source["netdFlushed"];
	        this.content = source["content"];
	        this.diagnostics = source["diagnostics"];
	    }
	}
	export class HostsStep {
	    enabled: boolean;
	    content: string;
	    flushDns: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HostsStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.content = source["content"];
	        this.flushDns = source["flushDns"];
	    }
	}
	export class IPTBackendInfo {
	    family: string;
	    available: boolean;
	    path: string;
	    version: string;
	    mode: string;
	    hasSave: boolean;
	    needsRoot: boolean;
	    readOnly: boolean;
	
	    static createFrom(source: any = {}) {
	        return new IPTBackendInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.family = source["family"];
	        this.available = source["available"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.mode = source["mode"];
	        this.hasSave = source["hasSave"];
	        this.needsRoot = source["needsRoot"];
	        this.readOnly = source["readOnly"];
	    }
	}
	export class IPTRule {
	    num: number;
	    pkts: number;
	    bytes: number;
	    target: string;
	    proto: string;
	    opt: string;
	    inIface: string;
	    outIface: string;
	    source: string;
	    dest: string;
	    extra: string;
	    raw: string;
	
	    static createFrom(source: any = {}) {
	        return new IPTRule(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.num = source["num"];
	        this.pkts = source["pkts"];
	        this.bytes = source["bytes"];
	        this.target = source["target"];
	        this.proto = source["proto"];
	        this.opt = source["opt"];
	        this.inIface = source["inIface"];
	        this.outIface = source["outIface"];
	        this.source = source["source"];
	        this.dest = source["dest"];
	        this.extra = source["extra"];
	        this.raw = source["raw"];
	    }
	}
	export class IPTChain {
	    name: string;
	    policy: string;
	    pkts: number;
	    bytes: number;
	    rules: IPTRule[];
	
	    static createFrom(source: any = {}) {
	        return new IPTChain(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.policy = source["policy"];
	        this.pkts = source["pkts"];
	        this.bytes = source["bytes"];
	        this.rules = this.convertValues(source["rules"], IPTRule);
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
	
	export class IPTSnapshot {
	    family: string;
	    table: string;
	    mode: string;
	    chains: IPTChain[];
	    restore: string;
	
	    static createFrom(source: any = {}) {
	        return new IPTSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.family = source["family"];
	        this.table = source["table"];
	        this.mode = source["mode"];
	        this.chains = this.convertValues(source["chains"], IPTChain);
	        this.restore = source["restore"];
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
	export class IptablesStep {
	    enabled: boolean;
	    v4Blob?: string;
	    v6Blob?: string;
	
	    static createFrom(source: any = {}) {
	        return new IptablesStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.v4Blob = source["v4Blob"];
	        this.v6Blob = source["v6Blob"];
	    }
	}
	export class LiveCaptureOptions {
	    maxPackets: number;
	    maxPcapBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new LiveCaptureOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxPackets = source["maxPackets"];
	        this.maxPcapBytes = source["maxPcapBytes"];
	    }
	}
	export class LiveCaptureState {
	    active: boolean;
	    iface: string;
	    bpf: string;
	    startedAt: number;
	    packets: number;
	    bytes: number;
	    pcapPath: string;
	    pcapBytes: number;
	    pcapRotations: number;
	    linkType: number;
	    maxPackets: number;
	    maxPcapBytes: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new LiveCaptureState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.active = source["active"];
	        this.iface = source["iface"];
	        this.bpf = source["bpf"];
	        this.startedAt = source["startedAt"];
	        this.packets = source["packets"];
	        this.bytes = source["bytes"];
	        this.pcapPath = source["pcapPath"];
	        this.pcapBytes = source["pcapBytes"];
	        this.pcapRotations = source["pcapRotations"];
	        this.linkType = source["linkType"];
	        this.maxPackets = source["maxPackets"];
	        this.maxPcapBytes = source["maxPcapBytes"];
	        this.error = source["error"];
	    }
	}
	export class LivePacketField {
	    k: string;
	    v: string;
	
	    static createFrom(source: any = {}) {
	        return new LivePacketField(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.k = source["k"];
	        this.v = source["v"];
	    }
	}
	export class LivePacketLayer {
	    name: string;
	    bytes: number;
	    offset: number;
	    fields: LivePacketField[];
	
	    static createFrom(source: any = {}) {
	        return new LivePacketLayer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.bytes = source["bytes"];
	        this.offset = source["offset"];
	        this.fields = this.convertValues(source["fields"], LivePacketField);
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
	export class LivePacketDetail {
	    no: number;
	    // Go type: time
	    ts: any;
	    length: number;
	    srcIP: string;
	    dstIP: string;
	    srcPort: number;
	    dstPort: number;
	    proto: string;
	    info: string;
	    layers: string[];
	    layersFull: LivePacketLayer[];
	    rawHex: string;
	
	    static createFrom(source: any = {}) {
	        return new LivePacketDetail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.no = source["no"];
	        this.ts = this.convertValues(source["ts"], null);
	        this.length = source["length"];
	        this.srcIP = source["srcIP"];
	        this.dstIP = source["dstIP"];
	        this.srcPort = source["srcPort"];
	        this.dstPort = source["dstPort"];
	        this.proto = source["proto"];
	        this.info = source["info"];
	        this.layers = source["layers"];
	        this.layersFull = this.convertValues(source["layersFull"], LivePacketLayer);
	        this.rawHex = source["rawHex"];
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
	
	
	export class LogcatStep {
	    enabled: boolean;
	    package: string;
	
	    static createFrom(source: any = {}) {
	        return new LogcatStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.package = source["package"];
	    }
	}
	export class NetIface {
	    name: string;
	    ipv4: string;
	    mac: string;
	    up: boolean;
	
	    static createFrom(source: any = {}) {
	        return new NetIface(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ipv4 = source["ipv4"];
	        this.mac = source["mac"];
	        this.up = source["up"];
	    }
	}
	export class NetworkInfo {
	    ip: string;
	    gateway: string;
	    dns: string[];
	    wifiSsid: string;
	    wifiBssid: string;
	    mac: string;
	    proxy: string;
	    interfaces: NetIface[];
	
	    static createFrom(source: any = {}) {
	        return new NetworkInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ip = source["ip"];
	        this.gateway = source["gateway"];
	        this.dns = source["dns"];
	        this.wifiSsid = source["wifiSsid"];
	        this.wifiBssid = source["wifiBssid"];
	        this.mac = source["mac"];
	        this.proxy = source["proxy"];
	        this.interfaces = this.convertValues(source["interfaces"], NetIface);
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
	export class ScrcpyStep {
	    enabled: boolean;
	    args: string[];
	
	    static createFrom(source: any = {}) {
	        return new ScrcpyStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.args = source["args"];
	    }
	}
	export class ProxyStep {
	    enabled: boolean;
	    hostPort: string;
	    port?: number;
	
	    static createFrom(source: any = {}) {
	        return new ProxyStep(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.hostPort = source["hostPort"];
	        this.port = source["port"];
	    }
	}
	export class Profile {
	    id: string;
	    name: string;
	    createdAt: number;
	    updatedAt: number;
	    frida: FridaStep;
	    forwards: ForwardsStep;
	    proxy: ProxyStep;
	    hosts: HostsStep;
	    cert: CertStep;
	    iptables: IptablesStep;
	    capture: CaptureStep;
	    logcat: LogcatStep;
	    scrcpy: ScrcpyStep;
	
	    static createFrom(source: any = {}) {
	        return new Profile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.frida = this.convertValues(source["frida"], FridaStep);
	        this.forwards = this.convertValues(source["forwards"], ForwardsStep);
	        this.proxy = this.convertValues(source["proxy"], ProxyStep);
	        this.hosts = this.convertValues(source["hosts"], HostsStep);
	        this.cert = this.convertValues(source["cert"], CertStep);
	        this.iptables = this.convertValues(source["iptables"], IptablesStep);
	        this.capture = this.convertValues(source["capture"], CaptureStep);
	        this.logcat = this.convertValues(source["logcat"], LogcatStep);
	        this.scrcpy = this.convertValues(source["scrcpy"], ScrcpyStep);
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
	
	
	
	export class ScrollbackEntry {
	    path: string;
	    serial: string;
	    label: string;
	    updatedAt: number;
	    bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new ScrollbackEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.serial = source["serial"];
	        this.label = source["label"];
	        this.updatedAt = source["updatedAt"];
	        this.bytes = source["bytes"];
	    }
	}
	export class Stats {
	    batteryLevel: number;
	    batteryTemp: number;
	    batteryVoltage: number;
	    charging: boolean;
	    memTotalKb: number;
	    memAvailKb: number;
	    cpuPercent: number;
	    loadAvg1: number;
	    storageTotalKb: number;
	    storageFreeKb: number;
	    uptimeSeconds: number;
	    netRxBytes: number;
	    netTxBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Stats(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.batteryLevel = source["batteryLevel"];
	        this.batteryTemp = source["batteryTemp"];
	        this.batteryVoltage = source["batteryVoltage"];
	        this.charging = source["charging"];
	        this.memTotalKb = source["memTotalKb"];
	        this.memAvailKb = source["memAvailKb"];
	        this.cpuPercent = source["cpuPercent"];
	        this.loadAvg1 = source["loadAvg1"];
	        this.storageTotalKb = source["storageTotalKb"];
	        this.storageFreeKb = source["storageFreeKb"];
	        this.uptimeSeconds = source["uptimeSeconds"];
	        this.netRxBytes = source["netRxBytes"];
	        this.netTxBytes = source["netTxBytes"];
	    }
	}
	export class StepPreview {
	    name: string;
	    title: string;
	    detail: string;
	    needsRoot: boolean;
	    willSkip: boolean;
	    skipReason?: string;
	
	    static createFrom(source: any = {}) {
	        return new StepPreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.needsRoot = source["needsRoot"];
	        this.willSkip = source["willSkip"];
	        this.skipReason = source["skipReason"];
	    }
	}
	
	export class TaskState {
	    id: string;
	    kind: string;
	    title: string;
	    detail: string;
	    status: string;
	    progress: number;
	    output: string;
	    err: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.title = source["title"];
	        this.detail = source["detail"];
	        this.status = source["status"];
	        this.progress = source["progress"];
	        this.output = source["output"];
	        this.err = source["err"];
	    }
	}
	export class TcpdumpAutoPlan {
	    abi: string;
	    url: string;
	    sha256: string;
	    source: string;
	    size: number;
	    cached: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TcpdumpAutoPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.abi = source["abi"];
	        this.url = source["url"];
	        this.sha256 = source["sha256"];
	        this.source = source["source"];
	        this.size = source["size"];
	        this.cached = source["cached"];
	    }
	}
	export class TcpdumpInfo {
	    path: string;
	    version: string;
	    exec: boolean;
	    source: string;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new TcpdumpInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.version = source["version"];
	        this.exec = source["exec"];
	        this.source = source["source"];
	        this.available = source["available"];
	    }
	}

}

export namespace main {
	
	export class DNSSnifferStatus {
	    running: boolean;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new DNSSnifferStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.source = source["source"];
	    }
	}
	export class ProcStreamStatus {
	    running: boolean;
	    intervalSec: number;
	
	    static createFrom(source: any = {}) {
	        return new ProcStreamStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.intervalSec = source["intervalSec"];
	    }
	}
	export class ProxySuggestion {
	    host: string;
	    port: number;
	    needsReverse: boolean;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxySuggestion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.port = source["port"];
	        this.needsReverse = source["needsReverse"];
	        this.reason = source["reason"];
	    }
	}

}

