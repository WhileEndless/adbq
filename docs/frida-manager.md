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

## 5. Sessions (canlı enstrümantasyon)

Apps ekranında **Start / Restart / Attach with Frida**:
- `StartAppWithFrida` (app.go) orkestrasyonu: bağlı scriptleri al → cihazda frida-server'ın çalıştığından emin ol (tek aday varsa otomatik başlat) → sürümü otoriter algıla → eşleşen host runtime'ı çöz (izin varsa yönetilen venv'i otomatik kur) → oturumu başlat.
- Driver (`internal/adb/frida_driver.py`, `go:embed`): job dosyasını okur, `get_device(serial)` ile bağlanır, spawn-suspended + attach (veya çalışan sürece attach), scriptleri yükler, resume eder. Satır başına bir compact JSON mesajı yayar (`console.log` → `log`, `send()` → `send`, hatalar → `error`).
- Durdurma: stdin kapatma (taşınabilir; Windows'ta SIGTERM yok) + süreç kill yedeği; driver çıkmadan önce detach eder (cihazda gum agent bırakmaz).
- Loglar **sekme kapalıyken de toplanır**: backend halka tamponu (5000) + monoton `seq`; frontend abone olunca `GetFridaSessionLog(sinceSeq)` ile backfill yapar ve `seq` ile tekilleştirir. Wails olayları fire-and-forget olduğundan, `resume`'dan ~50 ms sonra gelen ilk `console.log`'lar bu sayede kaçmaz.

İlgili: `internal/adb/frida_session.go`, `frontend/src/store.tsx` (frida slice), `frontend/src/screens/Frida.tsx`.

## Güvenlik notları

- İndirilen her şey **host-allowlist + SHA256** ile doğrulanır (wheel'ler `files.pythonhosted.org`, frida-server GitHub, CodeShare yalnızca kendi host'u).
- `pip` ağdan çözümleme yapmaz, sdist derlemez (`--no-index --only-binary=:all:`).
- npm bağımlılıkları tam-pin + `--ignore-scripts` + imza doğrulaması (`npm audit signatures`).
- Güven sınırı **attach anındadır**: indirilmiş bir CodeShare scripti, kullanıcı kaynağı görüp bir uygulamaya açıkça bağlayana kadar çalıştırılmaz.

## Manuel doğrulama (cihaz gerektirir)

`wails dev` ile: harici yorumlayıcı kaydı + onunla oturum (kurulum yolu hiç çalışmaz); yönetilen venv oluştur; rooted cihazda spawn vs attach; sürüm uyuşmazlığında tek tıkla yeniden kur; **loglar-sekme-kapalıyken** (oturum başlat → sekme değiştir → dön → backfill var, çift yok); cihazı çıkar → `dead` durumu, orphan python/gum agent yok; tek cihazda iki eşzamanlı oturum; CodeShare ara → gör → içe aktar → gözden geçir → bağla → çalıştır; dark/light editör + konsol.
