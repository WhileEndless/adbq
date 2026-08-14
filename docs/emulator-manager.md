# adbq — Emülatör Yöneticisi (Android SDK / AVD)

Sidebar'daki **Host → Emulators** bölümü, Android emülatör yaşam döngüsünün
tamamını adbq içinden yönetir: AVD listeleme/oluşturma/silme, donanım ayarlarını
düzenleme, system-image indirme/kaldırma, başlatma/durdurma ve — Play Store
imajları için — rootAVD ile Magisk kurulumu, ardından sistem CA sertifikası.

İlgili kod: `internal/adb/sdk.go`, `host_store.go`, `avd.go`, `avd_cmd.go`,
`avd_launch.go`, `avd_create.go`, `sdk_packages.go`, `rootavd.go`,
`rootavd_compat.go`, `hostlog.go`, `frontend/src/screens/Emulators.tsx`.

## 1. Sorun

adbq bugüne kadar yalnızca **zaten bağlı** cihazlarla ilgileniyordu. Emülatör
başlatmak, AVD oluşturmak, imaj indirmek için Android Studio'ya geçmek
gerekiyordu — üstelik pentest için en kullanışlı imaj olan **Google Play**
imajları `adb root`'u reddettiği için adbq'nun root gerektiren özelliklerinin
(frida, iptables, sistem sertifikası) çoğu o imajlarda hiç çalışmıyordu.

## 2. Host toolchain tespiti

`SDKManager` (`internal/adb/sdk.go`) SDK kökünü şu sırayla çözer:

1. Kullanıcının Ayarlar'dan verdiği yol (`~/.adbq/host.json`),
2. `ANDROID_HOME`,
3. `ANDROID_SDK_ROOT`,
4. platform varsayılanı (macOS `~/Library/Android/sdk`, Windows
   `%LOCALAPPDATA%\Android\Sdk`, Linux `~/Android/Sdk`).

Her aday `looksLikeSDK` ile **doğrulanır** — eski bir ortam değişkeni silinmiş
bir dizini gösteriyorsa sıradaki adaya düşülür, ölü bir yola bağlanılmaz.

`avdmanager`/`sdkmanager` Google tarafından iki kez taşındı; sırasıyla
`cmdline-tools/latest/bin`, sürümlü `cmdline-tools/<ver>/bin` ve eski
`tools/bin` denenir. PATH araması için frida tarafındaki `lookTool` yeniden
kullanılır (Finder'dan açılan uygulamanın dar PATH sorunu orada çözülmüştü).

Android Studio yalnızca **tespit** edilir, hiçbir zaman sürülmez: kurulu olması
SDK'nın nereden geldiğini açıklar. Sürüm `product-info.json`'dan okunur;
`dataDirectoryName` alanı kullanıcı için anlamlı sürümü (`2025.2.2`) verir,
ham build dizesini (`AI-252.27397…`) değil.

`~/.adbq/host.json` adbq'nun ilk genel kullanıcı-ayar dosyasıdır. Her alan bir
override'dır ve boş varsayılana sahiptir; dosyayı silmek otomatik tespite döner.
`Client.SetBinary` de ilk kez buraya bağlandı.

## 3. AVD envanteri

Kaynak `avdmanager list avd` **değil**, doğrudan `.ini` + `config.ini` çiftidir:
daha hızlı, çevrimdışı, command-line tools kurulu olmasa da çalışır ve daha
fazla bilgi verir.

İki tuzak açıkça ele alınır:

- **B1 — Ad ≠ dizin adı.** AVD'nin kimliği `.ini` dosyasının adıdır.
  `avdmanager` ikisini ayırabilir: `Medium_Phone_API_36.1.ini` →
  `Medium_Phone.avd`. `emulator -avd` yalnızca ilkini kabul eder.
- **B2 — Minor platform seviyeleri.** `target=android-36.1` artık mümkün. Düz
  bir `Atoi` bunu düşürür; `apiFromTarget` major seviyeyi (`36`) çıkarır, ham
  dizeyi `Target` alanında saklar.

Ayrıştırılamayan bir tanım **gizlenmez**, `Error` alanıyla listelenir — kaybolan
bir satır kullanıcıya "silinmiş" gibi görünür.

`config.ini`'den okunan alanlar: RAM, çekirdek, yoğunluk, çözünürlük, veri
bölümü, SD kart, GPU modu, cihaz profili, skin, tag, `PlayStore.enabled`, ABI,
`image.sysdir.1`. Snapshot'lar `<avd>/snapshots/` altındaki dizinlerden okunur;
disk kullanımı dizin ağacı toplanarak hesaplanır.

### 3.1. Durum modeli

`AVDState` beş değerlidir, bool değil:

| Durum | Anlamı |
|---|---|
| `stopped` | Süreç yok, adb transport yok |
| `booting` | Süreç var ama transport yok **ya da** transport var ama `sys.boot_completed=0` |
| `running` | Transport online ve boot tamamlanmış |
| `offline` | adb transport'u görüyor ama konuşmuyor — takılmış, beklenecek bir şey yok |
| `error` | Tanım okunamadı |

Soğuk boot'un ilk dakikasında "stopped" göstermek kullanıcıyı ikinci kez
başlatmaya davet ederdi; `booting` bu yüzden ayrı bir durumdur.

### 3.2. Donanım ayarlarını düzenleme

`AVDHardware` + `AVDHardwareChanges` (saf, testli) RAM, çekirdek, veri bölümü,
SD kart, GPU modu, çözünürlük, yoğunluk ve donanım klavyesini düzenler.
Değerler `config.ini`'ye yazılmadan **önce** doğrulanır: hatalı bir değer,
açılmayı reddeden ve anlaşılmaz bir hata veren bir emülatör üretir.

Düzenleme dosyaya **birleştirilir**, üzerine yazılmaz — Android Studio'nun
yazdığı ayarlar korunur. `hw.keyboard` alanı `*bool`'dur: aksi hâlde "kapat"
ile "düzenlenmedi" ayırt edilemezdi. Değişiklikler bir sonraki açılışta geçerli
olur ve UI bunu söyler.

## 4. Başlatma ve port tahsisi

Konsol portu **önceden** tahsis edilir (`-port <N>`, çift, 5554–5584). Bunun
sebebi tek: serial (`emulator-<N>`) boot bitmeden bilinsin. Aksi hâlde birden
fazla emülatör varken adbq hangisini başlattığını **tahmin etmek** zorunda
kalırdı.

`EmulatorArgs` **saftır** ve her seçenek ↔ bayrak eşlemesi birim testlidir; UI'da
gösterilen komut, çalışan komutun ta kendisidir (CLAUDE.md §4.1). Seçenekler
değiştikçe komut backend'den yeniden istenir, frontend'de elle birleştirilmez.

Desteklenen seçenekler: cold boot, snapshot seçimi/devre dışı bırakma, çıkışta
durumu atma, wipe data, headless (`-no-window`), boot animasyonu kapatma,
`-writable-system`, `-read-only`, GPU modu, RAM, çekirdek, netspeed/netdelay,
DNS, HTTP proxy, SELinux, serbest ek argümanlar.

Durdurma önce `adb -s emulator-N emu kill` (zarif), 8 sn sonra süreç öldürme.
**adbq yalnızca kendi başlattığı emülatörleri öldürür**; Android Studio'dan
açılmış bir emülatör listelenir ve konsol üzerinden durdurulabilir ama adbq
kapanınca hayatta kalır.

## 5. Emülatör logu

Emülatör çıktısı **logcat boru hattından ayrı**, bellekte sınırlı bir halkada
tutulur (`internal/adb/hostlog.go`): 1500 satır / 256 KB üst sınır. Bu log
takip edilmek için değil, başarısız bir açılışın sebebini görmek içindir.

Frontend her satır için event almaz; `EmulatorLog(name, sinceSeq)` ile **sıra
imleci** üzerinden kuyruk çeker ve yalnızca panel açıkken (2 sn'de bir). Kapalı
panelin maliyeti sıfırdır. `Clear` tamponu boşaltır ama sıra numarasını geri
sarmaz — eski imleç tutan bir istemciye zaten gördüğü satırlar verilmemelidir.

## 6. System image yönetimi

**Kurulu** imajlar SDK'nın `system-images/` ağacından okunur: anlık, çevrimdışı,
command-line tools gerekmeden. Bir dizinin gerçek paket sayılması için
`source.properties` bulunmalıdır (yarım silinmiş kalıntılar elenir).

**Kurulabilir** katalog `sdkmanager --list` ile alınır; Google'ın yayımladığı
tüm manifestleri çektiği için onlarca saniye sürer, bu yüzden 15 dakikalık TTL
ile cache'lenir ve açık bir "Refresh" düğmesi vardır. Birleştirmede **disk
kazanır**: manifest bayat ya da erişilemez olabilir, dosya sistemi olamaz.

İndirme ilerlemesi `sdkmanager`'ın ilerleme çubuğundan çıkarılır. Çubuk `\r` ile
yeniden çizildiği için tarayıcı `\n` **ve** `\r` üzerinden böler; aksi hâlde tüm
yüzdeler tek dev satır olarak en sonda gelirdi.

**Lisanslar asla otomatik kabul edilmez.** Kurulum boş stdin ile çalışır: kabul
edilmemiş bir lisans sonsuza kadar beklemek yerine yüksek sesle başarısız olur
ve hata mesajı kullanıcıyı `sdkmanager --licenses`'a yönlendirir.

Kaldırma yıkıcıdır: onay diyaloğunda komut gösterilir (CLAUDE.md §5).

## 7. AVD oluşturma

`avdmanager create avd -n … -k … [-d …] [-c …] [--force]`. Argüman üretimi saf
ve testlidir. İki tool davranışı özel olarak ele alınır:

- `-d` verilmediğinde `avdmanager` "custom hardware profile?" diye sorar ve
  sonsuza kadar bekler → stdin'e `no` beslenir.
- `avdmanager`'ın bayrağı olmayan ayarlar (RAM, çekirdek, veri bölümü, klavye,
  GPU) sonradan `config.ini` yazılarak uygulanır — Android Studio da aynısını
  yapar.

AVD adları ve SDK paket yolları bir komut satırına ulaşmadan **beyaz listeyle
doğrulanır**, böylece UI'dan gelen bir değer ek argümana dönüşemez. Snapshot
adları da yol ayracına karşı kontrol edilir (AVD dizini altında bir yola
dönüşüyorlar).

## 8. rootAVD ile rootlama

### 8.1. Ne zaman gerekir

`RootAVDAdvice` (saf, testli) karar verir:

| Durum | Sonuç |
|---|---|
| `adb root` zaten çalışıyor | **not-needed** — çalışan root her şeyi geçersiz kılar; paylaşılan imajı yamalamak sadece risk ekler |
| `su` zaten var / imaj zaten yamalı | **already** |
| Play Store olmayan imaj (çalışmıyor) | **not-needed** — debuggable imaj, açılınca `adb root` çalışacaktır |
| Play Store olmayan imaj, `adb root` reddedildi | **eligible** |
| API 28 | **unsupported** — Android 9'un system-as-root düzenini ramdisk yaması kapsamıyor |
| API < 25 | **unsupported** |
| API > 34 veya önizleme imajı | **risky** — upstream buraya kadar test etmiyor, ama yasak değil |
| Play Store imajı, API 25–34 | **eligible** |

API > 34 için **yasak değil "riskli"** denmesinin sebebi somut: bu geliştirme
sırasında kullanılan makinede API 36.1 Play Store imajı rootAVD tarafından
başarıyla yamalanmış (yanında `ramdisk.img.backup` duruyor). Sert bir blok
yanlış olurdu.

### 8.2. Güven modeli (CLAUDE.md §1.1/§1.2)

rootAVD **depoya dahil edilmez ve link edilmez**. Çalışma anında, kullanıcı
onayından sonra indirilir:

- URL **tek bir değişmez commit**'i adlandırır, branch'i değil.
- **`rootAVD.sh`** (adbq'nun çalıştırdığı tek dosya) ve **`Magisk.zip`** (bu
  ağaçta cihaza ulaşabilen tek ikili) koda gömülü SHA-256'larla karşılaştırılır.
  Uyuşmazlık **tüm indirmeyi siler** — doğrulanmamış bir ağaç, sonraki bir
  çalıştırmanın bulabileceği yerde bırakılmaz.
- İndirme host'u `gitlab.com` ile sınırlıdır (`downloadVerifiedAsset`).
- Açma sırasında yol kaçışı (zip-slip) **tüm arşivi reddettirir**, girdiyi
  atlatmaz. Sembolik bağlar ve aygıt düğümleri hiç yazılmaz. Toplam ve girdi
  başına boyut sınırlıdır.
- Hedef `<UserCacheDir>/adbq/rootavd/<commit>/` — atılabilir, kullanıcı tek
  tıkla silebilir.

**Arşivin kendi özeti bilerek sabitlenmez**: GitLab tarball'ları isteğe göre
yeniden üretir ve gzip çerçevelemesi bayt-kararlı değildir; onu zorlamak
bütünlükle ilgisi olmayan sebeplerle patlardı. İçindeki dosya özetleri
kararlıdır ve asıl önemli olan onlardır.

### 8.3. adbq'nun doğrulayamadıkları

Onay diyaloğunda **açıkça** yazılır (`RootAVDInfo.Disclosures`):

- rootAVD **GPL-3.0** lisanslı üçüncü parti bir araçtır.
- Host'taki **paylaşılan** system-image ramdisk'ini değiştirir — AVD'yi değil.
  Aynı imajı kullanan **her** AVD etkilenir. (İnsanları en çok şaşırtan madde.)
- Yanına bir ramdisk yedeği yazar; **Restore** orijinali geri koyar.
- Çalışırken GitHub'dan bir Magisk sürümü indirir. **Bu indirmeyi adbq
  doğrulayamaz** — script'in kendi indirmesidir.
- Sonunda AVD kapatılıp cold-boot edilir; çalışan emülatördeki kaydedilmemiş
  durum kaybolur.

### 8.4. Akış

1. AVD çalışmıyorsa başlatılır ve boot beklenir (rootAVD canlı cihaza push/pull yapar).
2. `bash rootAVD.sh <ramdiskRel>` — `cmd.Dir` rootAVD dizini, `ANDROID_HOME`
   açıkça set edilir, stdin'e `1\n` beslenir.
   rootAVD'nin Magisk sürüm sorusu `read -t 10`'dur ve boş girdide stable'a
   düşer; `1` vermek bunu **açıkça** yanıtlar ve 10 saniyelik ölü beklemeyi
   ortadan kaldırır.
3. Çıktı AVD'nin kendi log halkasına akar; özetler task detayına yazılır.
4. **adbq cold-boot'u kendisi yapar** — script yalnızca kullanıcıya "yeniden
   başlat" der.
5. `ForgetRootProbe(serial)` ile önbelleğe alınmış root tespiti temizlenir ve
   root **doğrulanır**. "Rootlandı" bir varsayım değil, bir gözlemdir.

`FAKEBOOTIMG` **kullanılmaz**: emülatör içindeki Magisk uygulamasında elle
dokunma gerektirir, bu da otomasyonun tam tersidir. Varsayılan yol başarısız
olursa hata mesajı bu seçeneği anlatır.

Geri alma: `rootAVD.sh <ramdiskRel> restore`, ayrı düğme, onayda komut görünür.

## 9. Sertifika zinciri (Burp / mitmproxy / ZAP)

**Yeni kod yazılmadı.** `internal/adb/certs.go:InstallSystemCert` zaten altı
strateji (direct / magisk-remount / remount-root / remount-system /
tmpfs-overlay / user-store) uyguluyor ve `ListCACerts` depoyu okuyor. AVD root
olduktan sonra yapılacak rootAVD'ye özgü hiçbir şey kalmıyor.

Bu yüzden `AlwaysTrustUserCerts` gibi üçüncü parti bir Magisk modülü
**kullanılmaz** — gereksiz olurdu ve yeni bir güvenilmez bağımlılık eklerdi.
`CertInstallResult.Persistent` false dönerse (tmpfs-overlay) UI "yeniden
başlatmada silinir" uyarısını basar; bu bilgi zaten sonuçta var.

## 10. Hata eşlemesi

| Ham çıktı | Kullanıcıya gösterilen |
|---|---|
| `License … not accepted` | Lisans kabul edilmemiş → `sdkmanager --licenses` |
| `Failed to find package` | SDK'da böyle bir paket yok, listeyi yenile |
| `UnknownHostException` | SDK deposuna erişilemedi, ağ/proxy kontrolü |
| `No space left on device` | Disk yetersiz |
| `Package path is not valid` | System image kurulu değil |
| `AVD … already exists` | Aynı adda AVD var |
| `JAVA_HOME is not set` | avdmanager Java runtime istiyor |
| `[!] No AVD is online` | Emülatör tam açılmamış |
| `Ramdisk.img uses UNKNOWN compression` | rootAVD bu imajın sıkıştırmasını tanımıyor |
| `Could not resolve host` (rootAVD) | Magisk indirilemedi |

## 11. Doğrulama

Saf birim testleri (SDK/cihaz gerektirmez):

```bash
go vet ./... && ADBQ_SKIP_DEVICE=1 go test ./...
```

Kapsam: `sdk_test.go` (yol çözümleme önceliği, sürüm ayrıştırma),
`avd_test.go` (ini parse, B1/B2 tuzakları, bozuk tanım, nil-slice),
`avd_cmd_test.go` (her seçenek ↔ bayrak, deterministik sıra, port/serial),
`avd_create_test.go` (oluşturma argümanları, ad doğrulama, donanım düzenleme
sınırları, `config.ini` birleştirme), `sdk_packages_test.go` (katalog
ayrıştırma, enjeksiyon reddi, ilerleme yüzdesi, disk-kazanır birleştirme),
`rootavd_test.go` (öneri matrisi, açıklama metinleri, zip-slip, hash reddi),
`hostlog_test.go` (satır/bayt sınırı, imleç, monotonluk).

Host testleri (opt-in, gerçek SDK gerekir):

```bash
ADBQ_PROBE_SDK=1 go test ./internal/adb/ -run TestHost -v
ADBQ_PROBE_SDK=1 ADBQ_PROBE_AVD=Pixel_8 go test ./internal/adb/ -run TestHostAVDLifecycle -v -timeout 15m
ADBQ_PROBE_ROOTAVD=1 go test ./internal/adb/ -run TestHostRootAVDDownload -v
```

Sonuncusu ağa çıkar ve `~/Library/Caches/adbq/rootavd/` altına ~11 MB yazar;
sabitlenmiş commit'in hâlâ çözüldüğünü ve hâlâ beklenen özetleri verdiğini
kanıtlar. Hiçbir system image'a dokunmaz.

Elle doğrulama (CLAUDE.md §4.3): `wails dev` → Host → Emulators.
