export namespace adb {
	
	export class AVD {
	    name: string;
	    display: string;
	    path: string;
	    target: string;
	    api: number;
	    androidVer: string;
	    tag: string;
	    tagDisplay: string;
	    playStore: boolean;
	    abi: string;
	    device: string;
	    deviceMfr: string;
	    skin: string;
	    ramMB: number;
	    cores: number;
	    density: number;
	    resolution: string;
	    sdCard: string;
	    dataSize: string;
	    gpuMode: string;
	    keyboard: boolean;
	    diskBytes: number;
	    sysImgDir: string;
	    ramdiskRel: string;
	    patched: boolean;
	    snapshots: string[] | null;
	    state: string;
	    serial: string;
	    port: number;
	    managed: boolean;
	    root: string;
	    error: string;
	    warning: string;
	    commands: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new AVD(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.display = source["display"];
	        this.path = source["path"];
	        this.target = source["target"];
	        this.api = source["api"];
	        this.androidVer = source["androidVer"];
	        this.tag = source["tag"];
	        this.tagDisplay = source["tagDisplay"];
	        this.playStore = source["playStore"];
	        this.abi = source["abi"];
	        this.device = source["device"];
	        this.deviceMfr = source["deviceMfr"];
	        this.skin = source["skin"];
	        this.ramMB = source["ramMB"];
	        this.cores = source["cores"];
	        this.density = source["density"];
	        this.resolution = source["resolution"];
	        this.sdCard = source["sdCard"];
	        this.dataSize = source["dataSize"];
	        this.gpuMode = source["gpuMode"];
	        this.keyboard = source["keyboard"];
	        this.diskBytes = source["diskBytes"];
	        this.sysImgDir = source["sysImgDir"];
	        this.ramdiskRel = source["ramdiskRel"];
	        this.patched = source["patched"];
	        this.snapshots = source["snapshots"];
	        this.state = source["state"];
	        this.serial = source["serial"];
	        this.port = source["port"];
	        this.managed = source["managed"];
	        this.root = source["root"];
	        this.error = source["error"];
	        this.warning = source["warning"];
	        this.commands = source["commands"];
	    }
	}
	export class AVDHardware {
	    ramMB: number;
	    cores: number;
	    dataSize: string;
	    sdCard: string;
	    gpuMode: string;
	    width: number;
	    height: number;
	    density: number;
	    keyboard?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AVDHardware(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ramMB = source["ramMB"];
	        this.cores = source["cores"];
	        this.dataSize = source["dataSize"];
	        this.sdCard = source["sdCard"];
	        this.gpuMode = source["gpuMode"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.density = source["density"];
	        this.keyboard = source["keyboard"];
	    }
	}
	export class AVDSpec {
	    name: string;
	    pkg: string;
	    device: string;
	    sdCard: string;
	    force: boolean;
	    ramMB: number;
	    cores: number;
	    dataSize: string;
	    keyboard: boolean;
	    gpuMode: string;
	
	    static createFrom(source: any = {}) {
	        return new AVDSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.pkg = source["pkg"];
	        this.device = source["device"];
	        this.sdCard = source["sdCard"];
	        this.force = source["force"];
	        this.ramMB = source["ramMB"];
	        this.cores = source["cores"];
	        this.dataSize = source["dataSize"];
	        this.keyboard = source["keyboard"];
	        this.gpuMode = source["gpuMode"];
	    }
	}
	export class AndroidSDKInfo {
	    available: boolean;
	    sdkRoot: string;
	    source: string;
	    emulator: string;
	    emulatorVer: string;
	    avdManager: string;
	    sdkManager: string;
	    adb: string;
	    avdHome: string;
	    studioPath: string;
	    studioVer: string;
	    accelerated: boolean;
	    accelNote: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new AndroidSDKInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.sdkRoot = source["sdkRoot"];
	        this.source = source["source"];
	        this.emulator = source["emulator"];
	        this.emulatorVer = source["emulatorVer"];
	        this.avdManager = source["avdManager"];
	        this.sdkManager = source["sdkManager"];
	        this.adb = source["adb"];
	        this.avdHome = source["avdHome"];
	        this.studioPath = source["studioPath"];
	        this.studioVer = source["studioVer"];
	        this.accelerated = source["accelerated"];
	        this.accelNote = source["accelNote"];
	        this.error = source["error"];
	    }
	}
	export class ApkInstallPlan {
	    file: string;
	    install: string[] | null;
	    skipped: string[] | null;
	    split: boolean;
	    commands: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new ApkInstallPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.install = source["install"];
	        this.skipped = source["skipped"];
	        this.split = source["split"];
	        this.commands = source["commands"];
	    }
	}
	export class AppVersion {
	    name: string;
	    code: string;
	
	    static createFrom(source: any = {}) {
	        return new AppVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.code = source["code"];
	    }
	}
	export class ApkSet {
	    pkg: string;
	    base: string;
	    splits: string[] | null;
	    split: boolean;
	    suggested: string;
	    commands: string[] | null;
	    version: AppVersion;
	
	    static createFrom(source: any = {}) {
	        return new ApkSet(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pkg = source["pkg"];
	        this.base = source["base"];
	        this.splits = source["splits"];
	        this.split = source["split"];
	        this.suggested = source["suggested"];
	        this.commands = source["commands"];
	        this.version = this.convertValues(source["version"], AppVersion);
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
	export class AppCommands {
	    pkg: string;
	    launch: string[] | null;
	    forceStop: string[] | null;
	    clear: string[] | null;
	    uninstall: string[] | null;
	    exportData: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new AppCommands(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pkg = source["pkg"];
	        this.launch = source["launch"];
	        this.forceStop = source["forceStop"];
	        this.clear = source["clear"];
	        this.uninstall = source["uninstall"];
	        this.exportData = source["exportData"];
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
	    splits: string[] | null;
	    flags: string[] | null;
	    privateFlags: string[] | null;
	    supportsScreens: string[] | null;
	    signature: string;
	    apkSigningVersion: string;
	    enabled: string;
	    installed: string;
	    stopped: string;
	    notLaunched: string;
	    suspended: string;
	    instant: string;
	    gids: string[] | null;
	    requestedPerms: string[] | null;
	    grantedPerms: GrantedPerm[] | null;
	
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
	export class AppScripts {
	    package: string;
	    scriptIds: string[] | null;
	    mode: string;
	    venvVer?: string;
	
	    static createFrom(source: any = {}) {
	        return new AppScripts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.package = source["package"];
	        this.scriptIds = source["scriptIds"];
	        this.mode = source["mode"];
	        this.venvVer = source["venvVer"];
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
	    steps: StepResult[] | null;
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
	export class BinaryPlan {
	    pkg: string;
	    suggested: string;
	    sources: number;
	    commands: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new BinaryPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pkg = source["pkg"];
	        this.suggested = source["suggested"];
	        this.sources = source["sources"];
	        this.commands = source["commands"];
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
	export class CodeshareProject {
	    owner: string;
	    slug: string;
	    title: string;
	    likes: string;
	    views: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeshareProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner = source["owner"];
	        this.slug = source["slug"];
	        this.title = source["title"];
	        this.likes = source["likes"];
	        this.views = source["views"];
	    }
	}
	export class CodeshareScript {
	    owner: string;
	    slug: string;
	    projectName: string;
	    description: string;
	    fridaVersion: string;
	    likes: number;
	    source: string;
	    sourceSha: string;
	
	    static createFrom(source: any = {}) {
	        return new CodeshareScript(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.owner = source["owner"];
	        this.slug = source["slug"];
	        this.projectName = source["projectName"];
	        this.description = source["description"];
	        this.fridaVersion = source["fridaVersion"];
	        this.likes = source["likes"];
	        this.source = source["source"];
	        this.sourceSha = source["sourceSha"];
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
	    rootPending: boolean;
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
	        this.rootPending = source["rootPending"];
	        this.ip = source["ip"];
	        this.wifi = source["wifi"];
	        this.mac = source["mac"];
	        this.hardwareSerial = source["hardwareSerial"];
	    }
	}
	export class DeviceProfile {
	    id: string;
	    name: string;
	    oem: string;
	    tag: string;
	    formFactor: string;
	    recommended: boolean;
	
	    static createFrom(source: any = {}) {
	        return new DeviceProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.oem = source["oem"];
	        this.tag = source["tag"];
	        this.formFactor = source["formFactor"];
	        this.recommended = source["recommended"];
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
	export class EmulatorOpts {
	    coldBoot: boolean;
	    noSnapshotSave: boolean;
	    noSnapshot: boolean;
	    snapshot: string;
	    wipeData: boolean;
	    noWindow: boolean;
	    noBootAnim: boolean;
	    writableSystem: boolean;
	    readOnly: boolean;
	    gpu: string;
	    memoryMB: number;
	    cores: number;
	    netSpeed: string;
	    netDelay: string;
	    dns: string;
	    httpProxy: string;
	    selinux: string;
	    extra: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new EmulatorOpts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.coldBoot = source["coldBoot"];
	        this.noSnapshotSave = source["noSnapshotSave"];
	        this.noSnapshot = source["noSnapshot"];
	        this.snapshot = source["snapshot"];
	        this.wipeData = source["wipeData"];
	        this.noWindow = source["noWindow"];
	        this.noBootAnim = source["noBootAnim"];
	        this.writableSystem = source["writableSystem"];
	        this.readOnly = source["readOnly"];
	        this.gpu = source["gpu"];
	        this.memoryMB = source["memoryMB"];
	        this.cores = source["cores"];
	        this.netSpeed = source["netSpeed"];
	        this.netDelay = source["netDelay"];
	        this.dns = source["dns"];
	        this.httpProxy = source["httpProxy"];
	        this.selinux = source["selinux"];
	        this.extra = source["extra"];
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
	    forwards: ForwardSpec[] | null;
	    reverses: ReverseSpec[] | null;
	
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
	    supported: string[] | null;
	
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
	export class FridaHistoryEntry {
	    package: string;
	    mode: string;
	    runtimeVer: string;
	    scriptIds: string[] | null;
	    scriptNames: string[] | null;
	    lastRun: number;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new FridaHistoryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.package = source["package"];
	        this.mode = source["mode"];
	        this.runtimeVer = source["runtimeVer"];
	        this.scriptIds = source["scriptIds"];
	        this.scriptNames = source["scriptNames"];
	        this.lastRun = source["lastRun"];
	        this.count = source["count"];
	    }
	}
	export class FridaHostInfo {
	    available: boolean;
	    pythonPath: string;
	    pythonVersion: string;
	    hasVenv: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new FridaHostInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.pythonPath = source["pythonPath"];
	        this.pythonVersion = source["pythonVersion"];
	        this.hasVenv = source["hasVenv"];
	        this.error = source["error"];
	    }
	}
	export class FridaMsg {
	    seq: number;
	    time: number;
	    kind: string;
	    script?: string;
	    level?: string;
	    payload?: string;
	    stack?: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new FridaMsg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.time = source["time"];
	        this.kind = source["kind"];
	        this.script = source["script"];
	        this.level = source["level"];
	        this.payload = source["payload"];
	        this.stack = source["stack"];
	        this.detail = source["detail"];
	    }
	}
	export class FridaRelease {
	    version: string;
	    arch: string;
	    assetURL: string;
	    size: number;
	    sha256: string;
	    installed: boolean;
	    advice: string;
	    adviceNote?: string;
	
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
	        this.advice = source["advice"];
	        this.adviceNote = source["adviceNote"];
	    }
	}
	export class FridaRuntime {
	    id: string;
	    kind: string;
	    label: string;
	    pythonPath: string;
	    fridaVersion: string;
	    pythonVersion: string;
	    addedAt?: number;
	
	    static createFrom(source: any = {}) {
	        return new FridaRuntime(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.label = source["label"];
	        this.pythonPath = source["pythonPath"];
	        this.fridaVersion = source["fridaVersion"];
	        this.pythonVersion = source["pythonVersion"];
	        this.addedAt = source["addedAt"];
	    }
	}
	export class FridaScript {
	    id: string;
	    name: string;
	    description: string;
	    origin: string;
	    codeshareOwner?: string;
	    codeshareSlug?: string;
	    sourceSha?: string;
	    trusted: boolean;
	    createdAt: number;
	    updatedAt: number;
	    source?: string;
	
	    static createFrom(source: any = {}) {
	        return new FridaScript(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.origin = source["origin"];
	        this.codeshareOwner = source["codeshareOwner"];
	        this.codeshareSlug = source["codeshareSlug"];
	        this.sourceSha = source["sourceSha"];
	        this.trusted = source["trusted"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	        this.source = source["source"];
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
	    ambiguous: boolean;
	    runnable: boolean;
	
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
	        this.ambiguous = source["ambiguous"];
	        this.runnable = source["runnable"];
	    }
	}
	export class FridaSessionInfo {
	    id: string;
	    serial: string;
	    package: string;
	    mode: string;
	    runtime: string;
	    startedAt: number;
	    status: string;
	    statusNote?: string;
	
	    static createFrom(source: any = {}) {
	        return new FridaSessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.serial = source["serial"];
	        this.package = source["package"];
	        this.mode = source["mode"];
	        this.runtime = source["runtime"];
	        this.startedAt = source["startedAt"];
	        this.status = source["status"];
	        this.statusNote = source["statusNote"];
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
	
	export class HostLogLine {
	    seq: number;
	    text: string;
	    err: boolean;
	
	    static createFrom(source: any = {}) {
	        return new HostLogLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.text = source["text"];
	        this.err = source["err"];
	    }
	}
	export class HostSettings {
	    sdkRoot?: string;
	    adbPath?: string;
	    avdHome?: string;
	    jadxPath?: string;
	    javaPath?: string;
	
	    static createFrom(source: any = {}) {
	        return new HostSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sdkRoot = source["sdkRoot"];
	        this.adbPath = source["adbPath"];
	        this.avdHome = source["avdHome"];
	        this.jadxPath = source["jadxPath"];
	        this.javaPath = source["javaPath"];
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
	    rules: IPTRule[] | null;
	
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
	    chains: IPTChain[] | null;
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
	export class JadxInfo {
	    installed: boolean;
	    kind: string;
	    bin: string;
	    dir: string;
	    version: string;
	    pinnedVersion: string;
	    source: string;
	    asset: string;
	    license: string;
	    sha256: string;
	    java: string;
	    javaVersion: string;
	    javaSource: string;
	    javaError: string;
	    ready: boolean;
	    disclosures: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new JadxInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.kind = source["kind"];
	        this.bin = source["bin"];
	        this.dir = source["dir"];
	        this.version = source["version"];
	        this.pinnedVersion = source["pinnedVersion"];
	        this.source = source["source"];
	        this.asset = source["asset"];
	        this.license = source["license"];
	        this.sha256 = source["sha256"];
	        this.java = source["java"];
	        this.javaVersion = source["javaVersion"];
	        this.javaSource = source["javaSource"];
	        this.javaError = source["javaError"];
	        this.ready = source["ready"];
	        this.disclosures = source["disclosures"];
	    }
	}
	export class JadxOpenPlan {
	    bin: string;
	    java: string;
	    names: string[] | null;
	    split: boolean;
	    staged: boolean;
	    ready: boolean;
	    reason: string;
	    commands: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new JadxOpenPlan(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.bin = source["bin"];
	        this.java = source["java"];
	        this.names = source["names"];
	        this.split = source["split"];
	        this.staged = source["staged"];
	        this.ready = source["ready"];
	        this.reason = source["reason"];
	        this.commands = source["commands"];
	    }
	}
	export class JadxRelease {
	    version: string;
	    asset: string;
	    sha256: string;
	    size: number;
	    published: string;
	    newer: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JadxRelease(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.asset = source["asset"];
	        this.sha256 = source["sha256"];
	        this.size = source["size"];
	        this.published = source["published"];
	        this.newer = source["newer"];
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
	    fields: LivePacketField[] | null;
	
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
	    layers: string[] | null;
	    layersFull: LivePacketLayer[] | null;
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
	    dns: string[] | null;
	    wifiSsid: string;
	    wifiBssid: string;
	    mac: string;
	    proxy: string;
	    interfaces: NetIface[] | null;
	
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
	
	
	export class RootAVDInfo {
	    installed: boolean;
	    dir: string;
	    script: string;
	    commit: string;
	    source: string;
	    archive: string;
	    license: string;
	    scriptSHA: string;
	    magiskSHA: string;
	    runner: string;
	    runnerNote: string;
	    disclosures: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new RootAVDInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.installed = source["installed"];
	        this.dir = source["dir"];
	        this.script = source["script"];
	        this.commit = source["commit"];
	        this.source = source["source"];
	        this.archive = source["archive"];
	        this.license = source["license"];
	        this.scriptSHA = source["scriptSHA"];
	        this.magiskSHA = source["magiskSHA"];
	        this.runner = source["runner"];
	        this.runnerNote = source["runnerNote"];
	        this.disclosures = source["disclosures"];
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
	
	export class SystemImage {
	    pkg: string;
	    level: string;
	    api: number;
	    androidVer: string;
	    tag: string;
	    abi: string;
	    playStore: boolean;
	    revision: string;
	    desc: string;
	    installed: boolean;
	    location: string;
	    rootable: boolean;
	    compatible: boolean;
	    note: string;
	    commands: string[] | null;
	
	    static createFrom(source: any = {}) {
	        return new SystemImage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pkg = source["pkg"];
	        this.level = source["level"];
	        this.api = source["api"];
	        this.androidVer = source["androidVer"];
	        this.tag = source["tag"];
	        this.abi = source["abi"];
	        this.playStore = source["playStore"];
	        this.revision = source["revision"];
	        this.desc = source["desc"];
	        this.installed = source["installed"];
	        this.location = source["location"];
	        this.rootable = source["rootable"];
	        this.compatible = source["compatible"];
	        this.note = source["note"];
	        this.commands = source["commands"];
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

