# adbq — APK dışa aktarma ve kurma (split / App Bundle dahil)

## 1. Sorun

Play Store'dan kurulan modern uygulamaların çoğu **App Bundle** kurulumudur:
cihazda tek bir `base.apk` değil, birden fazla APK bulunur.

```
/data/app/<pkg>-<hash>/base.apk
/data/app/<pkg>-<hash>/split_config.arm64_v8a.apk   # ABI
/data/app/<pkg>-<hash>/split_config.xxhdpi.apk      # ekran yoğunluğu
/data/app/<pkg>-<hash>/split_config.tr.apk          # dil
```

Yalnızca `base.apk`'yı çekip sonra kurmaya çalışmak başarısız olur:

```
adb: failed to install base.apk:
  Failure [INSTALL_FAILED_MISSING_SPLIT: Missing split for <pkg>]
```

(Bu çıktı, bağlı bir cihazdan alınan gerçek bir 4-APK'lık uygulamayla
doğrulanmıştır.)

Ayrıca split'ler **tek tek** kurulamaz; hepsi tek bir `pm` oturumunda
commit edilmelidir.

## 2. Çözüm

### 2.1. Dışa aktarma — `.apks`

`Client.ExportApks` (`internal/adb/apks.go`):

1. `pm path <pkg>` ile bütün APK yolları okunur.
2. Her biri geçici bir dizine `adb pull` ile çekilir (cihazdaki dosya adı korunur).
3. Hepsi tek bir zip'e paketlenir → `<pkg>.apks`.
4. Arşive `meta.sai_v2.json` eklenir; böylece dosya adbq dışında
   **SAI (Split APKs Installer)** gibi araçlarla da kurulabilir.

Arşiv düzeni (kökte, düz):

```
base.apk
split_config.arm64_v8a.apk
split_config.xxhdpi.apk
split_config.tr.apk
meta.sai_v2.json
```

**Yalnızca gerçekten split olan uygulamalar** `.apks` olur. Tek APK'lık bir
uygulama doğrudan `.apk` olarak çekilir — tek dosyayı arşive sarmak onu başka
araçlarla kurulamaz hâle getirirdi (`TestDeviceExportSingleApkIsNotAnArchiveWrapper`).

### 2.1.1. İmza bozulmaz

APK'lar **bayt bayt** kopyalanır: yeniden zip'lenmez, yeniden imzalanmaz,
hizalama (`zipalign`) değiştirilmez. Dışa aktarılan her APK'nın SHA-256'sı
cihazdaki dosyanınkiyle karşılaştırılarak doğrulanır
(`TestDeviceExportApks`). Dolayısıyla v1/v2/v3 imzaları geçerli kalır ve
arşiv aynı cihaza/başka cihaza sorunsuz kurulur. `.apks` sarmalayıcı zip'in
kendisi imzalı değildir — zaten `pm` onu değil, içindeki APK'ları doğrular.

### 2.2. Geri kurma — `install-multiple`

`Client.InstallApkBundle`:

1. `.apk` ise doğrudan `adb install -r`.
2. `.apks` / `.xapk` / `.zip` ise arşiv geçici dizine açılır.
3. **Cihaza uyan** APK'lar seçilir (aşağıya bakınız).
4. `base.apk` başa alınır — `pm`, oturumun paket adını **ilk** APK'dan
   türetir; başta bir config split olursa oturum reddedilir.
5. `adb install-multiple -r <apk…>` ile hepsi tek oturumda commit edilir.

### 2.3. Hangi split kurulur?

`selectApkEntries` saf fonksiyonu (birim testli):

| Split türü | Kural |
|---|---|
| base / feature modülü | her zaman kurulur |
| ABI (`arm64_v8a`, `x86`…) | yalnızca cihazın `ro.product.cpu.abilist` değeriyle eşleşenler |
| Ekran yoğunluğu (`xxhdpi`…) | cihazın dpi'sine **en yakın tek** kova; eşitlikte daha yüksek olan (`wm density`, yoksa `ro.sf.lcd_density`) |
| Dil (`tr`, `zh_CN`…) | hepsi kurulur (küçüktürler ve her zaman geçerlidir) |
| `nodpi` / `anydpi` | her zaman kurulur |

Cihaz yoğunluğu okunamazsa **hiçbir şey elenmez** (tahmin edip tek kaynak
split'i atmaktansa fazlasını kurmak yeğdir).

Elenen her dosya gerekçesiyle birlikte `ApkInstallPlan.Skipped` içinde
kullanıcıya gösterilir.

### 2.4. Yabancı arşivler

- **bundletool** çıktısı (`build-apks`): `toc.pb` + `splits/` + `standalones/`
  + `universal.apk` bir arada bulunur ve bunlardan **yalnızca biri**
  kurulabilir. `preferredApkEntries` sırayla `splits/` → kök → `universal.apk`
  → `standalones/` tercih eder.
- **`.xapk`**: kökte APK'lar + `manifest.json`. `.obb` dosyaları
  kurulmaz; "APK değil" gerekçesiyle atlandığı bildirilir.
- Zip girdileri **düzleştirilir** (yalnızca dosya adı kullanılır) — bu aynı
  zamanda zip-slip savunmasıdır.

## 3. Komut şeffaflığı

[`CLAUDE.md §4.1`](../CLAUDE.md) gereği her iki akış da çalıştırdığı komutu
gösterir (bkz. [`command-visibility.md`](command-visibility.md)):

- **Dışa aktarma**: Apps → uygulama detayı → *APK export* bölümü, uygulamanın
  düzenini (tek APK / App Bundle) ve `pm path` + her APK için `pull`
  satırlarını gösterir (`ApkSet.Commands`).
- **Kurma**: kurulum bir uygulamaya değil **cihaza** ait bir işlemdir; bu yüzden
  uygulama detayında değil, Apps ekranının başlığındaki *Install APK / APKS*
  düğmesinde (ve Overview hızlı eylemlerinde) durur. Dosya seçildikten sonra
  **kurulumdan önce** onay diyaloğu açılır; içinde `adb -s <serial>
  install-multiple -r …` komutu, kurulacak APK sayısı ve elenen dosyalar yer
  alır (`ApkInstallPlan`). Ortak akış: `frontend/src/lib/apk.tsx`.

## 4. Hata eşlemesi

`installMultipleErr` pm'in terse çıktısını eyleme dönüştürülebilir hâle getirir:

| pm çıktısı | Gösterilen |
|---|---|
| `INSTALL_FAILED_MISSING_SPLIT` | arşivde eksik split var; tekrar dışa aktar |
| `INSTALL_FAILED_VERSION_DOWNGRADE` | daha yeni sürüm kurulu; önce kaldır |
| `INSTALL_FAILED_UPDATE_INCOMPATIBLE` / `signatures do not match` | farklı anahtarla imzalı kopya kurulu; önce kaldır |
| `INSTALL_FAILED_NO_MATCHING_ABIS` | arşivde bu cihazın ABI'si için native kod yok |
| `unknown command` | adb sürümü split kurulumu (`install-multiple`) desteklemiyor |

Not: `adb install*` başarısız olsa bile sıklıkla **0 ile çıkar**; gerçek sinyal
çıktıdaki `Failure [...]` satırıdır (`pmResultErr`).

## 5. Doğrulama

Birim testleri: `internal/adb/apks_test.go` — split sınıflandırma, cihaz
eşleme, yoğunluk seçimi, bundletool düzeni, arşiv gidiş-dönüşü, hata eşlemesi.

Cihaz testleri (opt-in): `internal/adb/apks_device_test.go`

```bash
# Salt-okunur: bir split uygulamayı dışa aktarır ve arşivi doğrular
ADBQ_PROBE_SERIAL=<seri> go test ./internal/adb/ -run TestDeviceExportApks -v

# Gidiş-dönüş: kaynaktan dışa aktarır, HEDEF cihaza kurar, sonra kaldırır
ADBQ_PROBE_SERIAL=<kaynak> ADBQ_PROBE_INSTALL_SERIAL=<hedef> \
  go test ./internal/adb/ -run TestDeviceInstallApksRoundTrip -v

# Belirli bir paketi sabitlemek için
ADBQ_PROBE_APKS_PKG=<paket> …
```

Kurulum testi bilerek **ayrı bir hedef seri** ister: bir uygulamayı geldiği
telefona yeniden kurmak canlı bir değişikliktir; test yalnızca gözden
çıkarılabilir bir hedef (emülatör) açıkça verildiğinde çalışır.

## 7. Sonraki adım: analiz

Dışa aktardıktan sonra kodu okumak ve native binary'leri almak için
[`apk-analysis.md`](apk-analysis.md) — Apps ekranındaki **Open in jadx** ve
**Download binaries** eylemleri. Dosya adlarının sürüm taşıması da orada
(§5) anlatılıyor.
