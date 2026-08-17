# adbq — APK analizi: jadx ile açma ve binary toplama

Dışa aktarma tarafı [`apk-export-install.md`](apk-export-install.md)'de anlatılıyor.
Bu doküman, dışa aktardıktan **sonra** gelen iki adımı kapsar:

- **Open in jadx** — uygulamanın bütün APK'ları bu bilgisayara kopyalanır ve
  **hepsi birlikte** jadx-gui'ye verilir.
- **Download binaries** — aynı kopyalardan native kitaplıklar, gömülü
  çalıştırılabilirler ve yanlarındaki runtime blob'ları tek bir zip'e toplanır.

İkisi de Apps ekranındaki uygulama detayında, "Analysis" bölümünde.

## 1. Neden dışa aktarılan dosyayı doğrudan açmıyoruz

Dışa aktarma bir **`.apks` arşivi** üretir (split kurulumlarda). jadx'in kabul
ettiği girdi türleri arasında `.apks` **yok**; ayrıca amaç arşivi açmak değil,
parçaların tamamını tek oturumda birleştirmek. jadx'in kullanım satırı çoğul:

```
jadx[-gui] [command] [options] <input files> (.apk, .dex, .jar, …)
```

Bu yüzden `base.apk` + bütün split'ler ayrı girdi olarak verilir. Sadece
`base.apk` açmak, feature split içindeki kodu görünmez yapar.

## 2. Staging — `internal/adb/apk_stage.go`

Her iki özellik de APK'ları diskte tek tek dosya olarak ister; `ExportApks` ise
tek bir arşiv üretir. Bu yüzden ayrı bir kopyalama katmanı var:

```
<UserCacheDir>/adbq/apkwork/<pkg>-<versionCode>/base.apk
<UserCacheDir>/adbq/apkwork/<pkg>-<versionCode>/split_config.arm64_v8a.apk
```

- **Cache dizini**, çünkü atılabilir: `frida_paths.go`'daki kural — kullanıcının
  yazdığı veri `~/.adbq`, yeniden üretilebilen veri `UserCacheDir`.
- **versionCode ile anahtarlanır**: uygulama güncellendiğinde eski build sessizce
  yeniden kullanılmaz.
- Dosya zaten yerinde ve boyutu sıfırdan büyükse **tekrar çekilmez**
  (`StagedApks.Cached`) — aynı uygulamayı ikinci kez açmak bedava.
- `pkg` ve `versionCode` cihazdan gelir, yani **güvenilmez veridir**:
  `stageKey` her ikisini de tek bir yol parçasına indirger; ayırıcı veya `..`
  geçirmez (`TestStageKeyCannotEscapeItsDirectory`).
- Settings → jadx kartındaki **Clear** düğmesi tüm dizini siler.

## 3. jadx yönetimi — `internal/adb/jadx.go`

jadx bir **bağımlılık değil**, kullanıcının indirdiği harici bir araç — tıpkı
`frida-server` ve `rootAVD` gibi. Lisansı (Apache-2.0), bakım durumu ve
kullanımı CLAUDE.md §1.1'i karşılıyor; disiplin gereken kısım indirme.

### 3.1. Pinleme

```go
jadxVersion = "1.5.6"
jadxAsset   = "https://github.com/skylot/jadx/releases/download/v1.5.6/jadx-1.5.6.zip"
jadxSHA     = "545ea2be…"          // arşivin SHA-256'sı, kodda gömülü
```

Varsayılan yol **tek bir bilinen artifact** çeker. İzinli host'lar yalnızca
`github.com` ve `objects.githubusercontent.com`; indirme `downloadVerifiedAsset`
üzerinden yapılır (allowlist + SHA-256 + atomik yazma).

Arşiv açılırken:

- tek üst dizin (`jadx-<sürüm>/`) soyulur (`zipCommonPrefix`),
- her yol `safeJoin` ile kontrol edilir; **bir** kötü giriş tüm arşivi reddettirir,
- giriş başına ve toplam boyut sınırlanır,
- sembolik bağlar/aygıt düğümleri yazılmaz,
- `bin/` altındaki başlatıcılar çalıştırılabilir kalır,
- herhangi bir hata **dizini tamamen siler** — doğrulanamayan bir ağacın sonraki
  çalıştırmada bulunması engellenir.

### 3.2. Elle güncelleme

Kullanıcı pinin üstüne iki yoldan çıkabilir:

1. **Kendi kurulumunu göstererek** — `HostSettings.JadxPath`. Bu her şeyi yener
   ve `kind: "external"` olarak raporlanır. adbq bu kopyaya dokunmaz (Remove
   yalnızca kendi indirdiğini siler).
2. **"Check for a newer release"** — `api.github.com/repos/skylot/jadx/releases/latest`
   sorgulanır, `jadx-<sürüm>.zip` asset'i seçilir ve GitHub'ın asset başına
   yayınladığı **`digest`** (`sha256:…`) alınır. Onay diyaloğunda sürüm, kaynak ve
   digest gösterilir; indirme o digest'e karşı doğrulanır.
   **Digest yayınlanmamışsa kurulum reddedilir** — adbq'nun doğrulayamadığı bir
   şey kurulmaz, kullanıcıya "kendin kur, yolunu göster" denir.

adbq **kendi kendini güncellemez**; pin kodda kalır ve API'ye ulaşılamadığında
geçerli olan odur.

### 3.3. Java

jadx **Java 11+** ister ve macOS/Linux için JRE'li paket yayınlanmıyor. Sıra:

| Kaynak | Nereden |
|---|---|
| `user` | Settings'te elle verilen yol |
| `path` | PATH (+ Homebrew/MacPorts dizinleri, `lookTool`) |
| `JAVA_HOME` | ortam değişkeni |
| `java_home` | macOS `/usr/libexec/java_home -v 11+` |
| `studio` | Android Studio'nun içindeki JBR |

Studio'nun JBR'ı kasıtlı olarak listede: Android geliştiricilerinin makinesinde
sistem çapında bulunan Java genelde odur. `parseJavaVersion` hem `17.0.9` hem
`1.8.0_301` biçimini anlar — ikincisi 8 olarak okunur ve **reddedilir**; yalnızca
baştaki sayıya bakmak onu "sürüm 1" yapar ve sessizce kabul ettirirdi.

Hiçbiri yoksa adbq **JRE indirmez**; ne bulamadığını söyler ve Settings'te elle
yol verilmesini önerir.

### 3.4. Başlatma

```
JAVA_HOME=<home> <jadx-gui> <base.apk> <split…>
```

Java `argv`'ye sıkıştırılmaz: başlatıcı kendi yorumlayıcısını bulan bir kabuk
betiği, doğru olanı söylemenin yolu `JAVA_HOME`. Gösterilen satır çalıştırılanla
aynı ve terminale yapıştırılabilir (`JadxCommand`, saf ve birim testli).

Süreç **izlenmez**: ekran yansıtmanın penceresi adbq'ya aitken, dekompiler
kullanıcının oturumudur — onu başlatan task'tan uzun yaşamalı.

## 4. Binary toplama — `internal/adb/apk_binaries.go`

### 4.1. Neden

Kurulum yalnızca kod taşımaz. Cross-platform araç zincirleri uygulamanın
tamamını bir native kitaplığa derler ve o kitaplığın açılışta yüklediği veri
blob'larını yanına koyar. APK'yı çekmek konteyneri verir; bunlara ulaşmak split
kurulumun her parçasını elle açmak demektir.

### 4.2. Hangi girişler

Karar **içeriğe bakılarak** verilir, ada güvenilerek değil:

| Kural | Kind |
|---|---|
| `lib/` altında ve `.so` ile bitiyor | `so` |
| içerik `\x7fELF` ile başlıyor | `elf` |
| içerik `MZ` ile başlıyor (yönetilen assembly'ler) | `pe` |
| dosya adı bilinen blob listesinde (`kernel_blob.bin`, `isolate_snapshot_data`, `vm_snapshot_data`, `global-metadata.dat`, …) | `blob` |

Uzantıya güvenmemenin sebebi: gömülen çalıştırılabilirler rutin olarak `.so`
(paketleme araçlarının geçirdiği tek uzantı), `.bin` ya da uzantısız gelir —
uzantı listesi tam da işe yarayan dosyaları kaçırır.

Dışarıda kalanlar (APK dışa aktarma bunları zaten kapsıyor): `classes*.dex`,
`AndroidManifest.xml`, `resources.arsc`, `res/**`, `META-INF/**`.

Ayrıca platformun **çıkardığı** kitaplık dizini de çekilir
(`<apk dizini>/lib`). Yok olması normaldir — `extractNativeLibs=false` olan
uygulamada hiç oluşmaz; okunamaması **hata değil**, manifest'e not düşülür.

### 4.3. Çıktı

```
<pkg>-<sürüm>-binaries.zip
├── base.apk/lib/arm64-v8a/libapp.so
├── split_config.arm64_v8a.apk/lib/arm64-v8a/libflutter.so
├── device-lib/arm64/…
└── manifest.json
```

Girişler **kaynak APK'ya göre gruplanır**; iki split'teki aynı adlı kitaplık
böyle ayırt edilebilir. `manifest.json` her girişin kaynağını, yolunu, boyutunu
ve SHA-256'sını taşır. Baytlar olduğu gibi kopyalanır.

Toplanacak hiçbir şey yoksa arşiv yine yazılır ve `manifest.json` bunu söyler —
sessiz bir başarısızlık ya da boş dosya bırakmaktan iyisi budur. Hata halinde
hedef dosya **silinir**: yarım yazılmış bir arşiv, tam göründüğü için yoktan
kötüdür.

## 5. Dosya adlarında sürüm — `internal/adb/export_name.go`

Bütün dışa aktarmalar artık sürümü de taşıyor:

```
com.example.app-1.2.3-10203.apks
com.example.app-1.2.3-10203-binaries.zip
com.example.app-1.2.3-10203.tar.gz
```

Sebep: paket adı tek başına iki kaydı ayırt etmeye yetmiyor — aynı uygulamayı
güncellemeden önce ve sonra çekince ikinci dosya birincisini eziyor ya da
yanına hangi build olduğunu söylemeyen bir adla oturuyor.

`ExportBaseName(pkg, AppVersion)` saf ve birim testli. Alternatif senaryolar:

| Durum | Sonuç |
|---|---|
| ikisi de var | `pkg-<versionName>-<versionCode>` |
| yalnızca versionName | `pkg-<versionName>` |
| yalnızca versionCode | `pkg-<versionCode>` |
| hiçbiri okunamadı | `pkg` (eski davranış) |
| versionName == versionCode | bir kez yazılır |
| versionName'de boşluk/`/`/parantez | tek `-`'ye indirgenir: `1.0 (beta)` → `1.0-beta` |
| versionName absürt uzun | 40 karaktere kırpılır |

Sürüm bilgisi `ApkSet.Version` alanında taşınır, böylece aynı uygulama için
başka bir dosya adı üretmek için cihaz **tekrar okunmaz**.

## 6. Komut şeffaflığı (CLAUDE.md §4.1)

Her iki eylem de çalıştıracağı komutu önden gösterir; metni backend üretir:

| Tip | Nerede | Ne gösteriyor |
|---|---|---|
| `JadxOpenPlan.Commands` | `jadx_open.go` | `pm path`, APK başına `pull`, `JAVA_HOME=… jadx-gui <girdiler>` |
| `BinaryPlan.Commands` | `apk_binaries.go` | `pm path`, APK başına `pull`, `pull …/lib`, ve toplama adımını anlatan `#` satırı |

Saf üreticiler: `JadxCommand`, `PlanAppBinaries`. İndirme onayındaki metin
(`JadxInfo.Disclosures`) da backend'de üretilir, böylece diyalog bir maddeyi
sessizce düşüremez.

## 7. Testler

| Dosya | Kapsam |
|---|---|
| `apk_stage_test.go` | staging anahtarı, dizin kaçışı |
| `jadx_test.go` | Java sürüm ayrıştırma (8 reddi dahil), Studio JBR yolları, komut alıntılama, çözümleme sırası, disclosure içeriği, arşiv soyma/traversal reddi, digest'siz release reddi |
| `apk_binaries_test.go` | sınıflandırma (ELF/PE/blob/atlananlar), sentetik zip üzerinde tarama, kaynağa göre gruplama + digest'ler, boş sonuç |
| `export_name_test.go` | dosya adı senaryoları ve sanitizasyon |
| `jadx_host_test.go` | `ADBQ_PROBE_JADX=1` ile gerçek indirme + pinin güncelliği |
