# Frida Manager

adbq'nun cihaz tarafındaki `frida-server` yönetimini (indir/doğrula/push/başlat — `internal/adb/frida.go`, `frida_install.go`) tamamlayan **host tarafı** katman. Amaç: doğru host araçlarını kurmak, script yönetmek, scriptleri uygulamalara bağlamak, enstrümante başlatmak ve çıktıyı canlı izlemek — hepsi adbq içinden.

Frida ekranı sekmeleri: **Server · Runtime · Scripts · App Scripts · Sessions**.

## 1. Runtime (host frida)

Enstrümantasyonu sürmek için host'ta `frida` Python paketi gerekir ve **sürümü cihazdaki `frida-server` ile eşleşmelidir** (major eşleşmezse `frida.ProtocolError`). İki birbirinin yerine geçebilen mod:

- **Yönetilen venv** — adbq, cihazdaki çalışan sunucu sürümünü **otoriter** biçimde algılar (`<server> --version`; dosya adına güvenmez), bir venv açar ve host'a uygun **tek wheel**'i kurar: PyPI JSON'dan SHA256 doğrulanır, `pip install --no-index --no-deps --only-binary=:all:` ile çevrimdışı kurulur. Sürüm başına bir venv.
- **Harici yorumlayıcı (bring-your-own)** — kullanıcı `frida`'yı kendisi kurar (`pip install frida==X.Y.Z`) ve venv/yorumlayıcı yolunu kaydeder; adbq **hiçbir şey kurmaz**, sadece sürüm bilgisini okur. Lisans açısından tamamen temiz yol.

Depolama (kod tabanının cache-vs-config ayrımına uyar):
- `<UserCacheDir>/adbq/frida/venvs/<sürüm>/` — yönetilen venv'ler (atılabilir).
- `<UserCacheDir>/adbq/frida/wheels/` — doğrulanmış wheel önbelleği.
- `~/.adbq/frida/runtime.json` — kayıtlı harici yorumlayıcılar + `managedEnabled`.

İlgili: `internal/adb/frida_tools.go`, `frida_paths.go`. Sürüm algılama: `frida.go:DetectRunningFridaVersion`.

## 2. Scripts (kütüphane + editör)

Cihazdan bağımsız JS script kütüphanesi. Her scriptin gövdesi `~/.adbq/frida/scripts/<id>.js` sidecar dosyası, metadata `scripts.json`'da (`internal/adb/frida_scripts.go`, `ProfileStore` desenini izler, atomik yazım). Editör **CodeMirror 6**; tema tamamen uygulamanın CSS değişkenleriyle sürülür (dark/light otomatik).

## 3. CodeShare

`codeshare.frida.re` entegrasyonu (`internal/adb/codeshare.go`):
- **Kaynak çekme** belgelenmiş JSON API ile: `GET /api/project/<owner>/<slug>/` — script gövdesi için otoriter.
- **Arama/gözatma** HTML kazıma (JSON arama API'si yok); markup değişirse hata değil **sıfır sonuç** döner, import-by-slug çalışmaya devam eder.
- Tüm istekler `codeshare.frida.re` host'una sabitlenir.
- İndirilen kaynak **güvenilmezdir**: editörde gösterilir, indirildiğinde **çalıştırılmaz**, `trusted=false` olarak saklanır. `sha256(source)` kaydedilir; yeniden çekmede yukarı-akış değişikliği saptanır.

## 4. App Scripts (uygulama → script bağlama)

Bağlamalar **paket-anahtarlı** (cihazdan bağımsız): bir paketin script seti + modu her cihazda aynıdır. `app-scripts.json`'da `{scriptIds, mode}` olarak tutulur. İki yüz:
- Apps ekranında bir uygulamanın detayında **Manage scripts**.
- Frida → **App Scripts** sekmesinde tüm bağlamaların merkezi görünümü.

## 4.5. Cihaz tarafı: root, port ve sunucu logu

**Root.** `adb root` emülatörlerde ve `ro.debuggable=1` cihazlarda **otomatik denenir**
(`internal/adb/adb_root.go`), serial başına bir kez, sonuç mandallanır. Stok bir
google_apis AVD'de `su` shell kullanıcısını reddeder — dolayısıyla `suStyleFor`'un
denediği dört formun hepsi başarısız olur — ama `adb root` anında tam root verir; bu
adım olmadan frida-server dahil tüm ayrıcalıklı özellikler emülatörde ölüdür.
Production imajı reddeder ve bir daha sorulmaz. `adb root` adbd'yi yeniden başlatıp
açık adb akışlarını (logcat, pcap) düşürdüğü için yalnızca cihaz keşfi sırasındaki ilk
root probe'undan tetiklenir.

`Device.Root` artık **yalnızca** su'nun gerçekten komut çalıştırdığı anlamına gelir.
Magisk izi tek başına `RootPending` üretir (izin bekliyor) — eskiden `Root=true`
diyordu ve UI, her çağrıda hata veren root-only aksiyonları açıyordu.

**Başlatma.** `-D` ile daemon olan frida-server, adb shell'in stdin/stdout/stderr
fd'lerini devralır ve adbd bağlantıyı kapatmaz; bu yüzden komut daemon yaşadığı sürece
dönmezdi. Üç fd de ayrılır (`</dev/null >>LOG 2>&1`) ve yerel bir timeout eklenir.

**Sunucu logu.** Çıktı `/data/local/tmp/adbq-frida-<port>.log` dosyasına yazılır ve
Server sekmesinde gösterilir. **Port başına** ayrıdır: aynı cihazda birden fazla
sunucu (farklı portlarda) çalışabilir ve tek dosya her başlatmada diğerinin
teşhisini siler. SELinux reddi, dolu port, mimari uyumsuzluğu ve ART'ı eşleyemeyen
agent yalnızca burada görünür.

**Port.** frida'nın Android arka ucu cihazda **yalnızca 27042**'yi dener
(`droidy-host-session.vala`, `DEFAULT_CONTROL_PORT`), ve frida-python bunu
değiştirmeye izin vermez. Başka bir portta sunucu "erişilemez" değil **görünmez**
olur; hata `need Gadget to attach on jailed Android` diye çıkar. adbq bu durumda
`adb forward tcp:0 tcp:<port>` ile oturum başına bir host portu açar ve driver oraya
`add_remote_device` ile bağlanır (`internal/adb/frida_remote.go`); oturum bitince
forward kaldırılır. Çalışan sunucunun portu `/proc/<pid>/cmdline`'dan okunur.

**Sunucu seçimi.** Cihazda birden fazla binary varsa tek-tık akışı artık seçim yapar:
cihazın çalıştırabileceği mimari → host'ta zaten eşleşen frida sürümü (venv kurmaya
gerek yok) → daha önce kullanılan → en yeni. `.xz` gibi arşivler ve çalıştırma biti
olmayan dosyalar `runnable=false` işaretlenir.

**Sürüm uyumu.** `FridaAdvice(sdk, version)` (`internal/adb/frida_compat.go`) release
listesine `advice` alanı ekler: Android 5.x'te (API ≤22) frida ≥16.6 **broken**
(agent ART'ı eşleyemez — API 21 emülatöründe ölçüldü: sunucu ayakta, `Unable to find
fields in java/lang/Thread`, tüm çağrılar timeout), API ≥35'te 16 KB sayfa boyutu
uyarısı. Uyumsuz bir sürüm indirilmeden önce onay istenir.

## 5. Sessions (canlı enstrümantasyon)

Apps ekranında **Start / Restart / Attach with Frida**:
- `StartAppWithFrida` (app.go) orkestrasyonu: bağlı scriptleri al → cihazda frida-server'ın çalıştığından emin ol (tek aday varsa otomatik başlat) → sürümü otoriter algıla → eşleşen host runtime'ı çöz (izin varsa yönetilen venv'i otomatik kur) → oturumu başlat.
- Driver (`internal/adb/frida_driver.py`, `go:embed`): job dosyasını okur, `get_device(serial)` (veya özel portta `add_remote_device`) ile bağlanır, spawn-suspended + attach (veya çalışan sürece attach), scriptleri yükler, resume eder. Satır başına bir compact JSON mesajı yayar (`console.*` → `log`, `send()` → `send`, hatalar → `error`).
- **Seçilen tüm scriptler tek bir frida agent'ı olarak yüklenir.** frida her script'e ayrı bir JS realm verdiği için script başına bridge prologue'u, hedef sürece **frida-java-bridge'in ikinci bir kopyasını** koyuyordu; aynı anda ART'ı yamalayan iki bridge süreci öldürüyordu. API 34 emülatöründe ölçüldü: tek Java scripti çalışır, iki veya üç tanesi hiçbir çıktı vermeden süreci sonlandırır — "birden fazla script seçince uygulama açılmıyor" şikâyeti tam olarak budur. `frida` CLI'nin birden çok `-l` dosyasıyla yaptığı da budur. Birleştirme, frida'nın mesaj başına damgaladığı script kimliğini götürdüğü için her gövde kendi kapsamında çalışır ve `console`/`send`'i etiketli shim'lerle gölgelenir; driver etiketi çözüp adı geri koyar, hatalar ise birleşik kaynak üzerindeki satır indeksiyle doğru script'e atanır. Shim'ler frida'nın `console` biçimlendirmesini birebir taklit eder (ArrayBuffer → hexdump, Error → stack, `undefined`). **Sonuç:** herhangi bir script'teki söz dizimi hatası hepsini birden düşürür (hata mesajı hepsini adlandırır), ve iki script `rpc.exports` atarsa artık çakışırlar.
- `console.*` çıktısı frida-python'ın `message` geri çağrısına **hiç uğramaz** — `Script._on_message` `type=="log"` mesajlarını script'in log handler'ına yönlendirir ve varsayılan handler bunu doğrudan stdout/stderr'e basar. Bu yüzden `set_log_handler` kurulur; aksi hâlde `console.warn`/`console.error` stderr'de kaybolur, `console.log` ise JSON protokolünü atlayarak ham satır olarak akar.
- Anlaşılmaz frida hataları çevrilir: `need Gadget to attach on jailed Android` → "sunucu çalışmıyor ya da bu portu dinlemiyor", `unable to detect any VM heap candidates` → "bu sunucu sürümü bu cihazı enstrümante edemiyor".
- Durdurma: stdin kapatma (taşınabilir; Windows'ta SIGTERM yok) + süreç kill yedeği; driver çıkmadan önce detach eder (cihazda gum agent bırakmaz).
- Loglar **sekme kapalıyken de toplanır**: backend halka tamponu (5000) + monoton `seq`; frontend abone olunca `GetFridaSessionLog(sinceSeq)` ile backfill yapar ve `seq` ile tekilleştirir. Wails olayları fire-and-forget olduğundan, `resume`'dan ~50 ms sonra gelen ilk `console.log`'lar bu sayede kaçmaz.
- Teslimat **kayıpsız ve toplu**: halka tamponu tek doğruluk kaynağıdır, `app.go` 100 ms'de bir `LogSince(last)` ile boşaltıp tek olayda dizi gönderir. Her mesaj için ayrı olay yayan eski yol, her çağrıda log basan bir hook'un altında olay köprüsünün gerisinde kalıp satırları sessizce düşürüyordu.

- Konsol okuması Logcat ekranıyla aynı davranır: **debounce'lu arama** (eşleşmeler `<mark>` ile vurgulanır), **tür süzgeci** (Logs · Sends · Warnings · Errors · Events), görünen satırlar için **Export**, ve **auto-scroll devri** — yukarı kaydırmak takibi bırakır, "Jump to latest" pili o sırada kaç satır geldiğini söyler, en alta dönmek takibi geri açar. Arama/süzgeç yalnızca görünümü etkiler; halka tamponu dokunulmaz kalır, süzgeci kaldırınca satırlar geri gelir.
- Arama ekranda **görünen** metinle eşleşir: `fridaRow()` her mesajı (ham `payload`, yaşam-döngüsü satırları ve `stack`) tek bir metne indirir; süzgeç, vurgulama ve export aynı fonksiyondan geçer, böylece `detached` gibi yalnızca türetilmiş satırlarda geçen bir kelime de bulunur. Son tür süzgeci kapatılamaz — hepsi kapalı bir konsol, kullanıcının kurduğu süzgeç değil bozuk oturum gibi görünür.

İlgili: `internal/adb/frida_session.go`, `frontend/src/store.tsx` (frida slice), `frontend/src/screens/Frida.tsx`, `frontend/src/lib/logSearch.tsx` (Logcat ile paylaşılan vurgulama).

## Java/ObjC/Swift bridge sürümü

Frida 17 bridge'leri agent'ın (cihazdaki `frida-server`) çekirdek sürümüne bağlıdır ve **uyumsuzluk sessizdir**: `Java.use` sınıfı bulur, `.implementation = fn` atanır, ama metot hiç yakalanmaz — script tek satır log basmaz. Bu yüzden bridge'i "PyPI'daki en yeni frida-tools"tan almak yanlıştır.

Kural (`resolveFridaToolsVersion`): çalışma zamanının frida sürümünün karşıladığı **en yüksek `frida>=` tabanına sahip sürüm satırının ilk sürümü** seçilir. frida-tools yama sürümleri tabanı yükseltmeden daha yeni bir `frida-java-bridge` yayımlayabiliyor — 14.5.0 ve 14.5.2 ikisi de `frida>=17.5.0` der, ama 17.5.1 agent altında yalnız 14.5.0'ın bridge'i hook atar. Önbellek `frida-tools-version.txt` ile damgalanır; damgadaki politika satırı değişirse önbellek kendini yeniler.

## Güvenlik notları

- İndirilen her şey **host-allowlist + SHA256** ile doğrulanır (wheel'ler `files.pythonhosted.org`, frida-server GitHub, CodeShare yalnızca kendi host'u).
- `pip` ağdan çözümleme yapmaz, sdist derlemez (`--no-index --only-binary=:all:`).
- npm bağımlılıkları tam-pin + `--ignore-scripts` + imza doğrulaması (`npm audit signatures`).
- Güven sınırı **attach anındadır**: indirilmiş bir CodeShare scripti, kullanıcı kaynağı görüp bir uygulamaya açıkça bağlayana kadar çalıştırılmaz.

## Otomatik doğrulama (cihaz gerektirir)

`internal/adb/frida_device_test.go` — `ADBQ_PROBE_SERIAL` ile opt-in, CI'da atlanır:

```
ADBQ_PROBE_SERIAL=emulator-5554 \
ADBQ_PROBE_FRIDA_PATH=/data/local/tmp/frida-server-<sürüm>-android-arm64 \
go test ./internal/adb -run Probe -v
```

Kapsam: capability/su-style raporu, envanterin tutarlılığı (arşiv `runnable` olamaz,
iki sunucu aynı portta aktif görünemez, aktif sunucu port bildirmek zorunda) ve
başlatmanın **asılmadan** dönüp sunucuyu gerçekten ayağa kaldırması.

## Manuel doğrulama (cihaz gerektirir)

`wails dev` ile: harici yorumlayıcı kaydı + onunla oturum (kurulum yolu hiç çalışmaz); yönetilen venv oluştur; rooted cihazda spawn vs attach; sürüm uyuşmazlığında tek tıkla yeniden kur; **loglar-sekme-kapalıyken** (oturum başlat → sekme değiştir → dön → backfill var, çift yok); cihazı çıkar → `dead` durumu, orphan python/gum agent yok; tek cihazda iki eşzamanlı oturum; CodeShare ara → gör → içe aktar → gözden geçir → bağla → çalıştır; dark/light editör + konsol.

Ek olarak: **bir uygulamaya iki Java scripti bağla** → uygulama açılmalı ve her ikisi
de loglamalı; **27042 dışı bir portta** sunucu başlat → oturum çalışmalı ve
`adb forward --list` oturum bittiğinde temiz olmalı; **stok emülatörde** adbq açılır
açılmaz root özellikleri çalışmalı (elle `adb root` gerekmemeli).
