# adbq — Proje Kuralları

Bu dosya, bu depoda çalışan tüm ajanlar (Claude Code dahil) için bağlayıcı kurallar içerir. Kod yazmadan önce **mutlaka** oku.

## 1. Bağımlılık & Güvenlik Kuralları

Bu projede kullanılacak her kütüphanenin **güvenilir** ve **bakımlı** olduğundan emin olunmalıdır. Aşağıdaki kontroller yapılmadan hiçbir paket eklenemez.

### 1.1. Bir kütüphane eklemeden önce DOĞRULA

- **Kaynak**: Yalnızca aşağıdakilerden gelen paketleri ekle:
  - Resmi Go modül kayıt defteri (`pkg.go.dev`) üzerinden ulaşılabilir,
  - Public GitHub/GitLab deposu kimliği doğrulanabilir bir organizasyona ait,
  - npm paketleri için scoped (`@org/paket`) ya da bilinen, yüksek indirme sayısına sahip paketler.
- **Bakım durumu**:
  - Son commit son 12 ay içinde olmalı,
  - Açık kritik güvenlik issue'ları olmamalı,
  - GitHub stars + kullanım göstergesi anlamlı olmalı (genellikle 500+).
- **Lisans uyumu**: MIT, BSD, Apache-2.0, MPL-2.0 kabul. GPL/AGPL/özel lisans varsa **önce kullanıcıya sor**.
- **Typosquatting kontrolü**: Paket adını yanlış yazılmış varyantlardan (örn. `requets`, `loadsh`) ayırt et. Doğru URL'i `pkg.go.dev` / `npmjs.com` üzerinden teyit et.
- **Güvenlik açığı taraması**:
  - Go için: `govulncheck ./...`
  - npm için: `npm audit --omit=dev`
  - Eklemeden önce çalıştır; yüksek/kritik açık varsa ekleme.

### 1.2. Yasak davranışlar

- **ASLA** `curl | sh`, `wget | bash` gibi imzasız betikler indirip çalıştırma.
- **ASLA** kullanıcı onayı olmadan global yüklemeler yapma (`npm i -g`, `go install` istisna: Wails CLI gibi proje gereksinimleri kullanıcıya açık şekilde belirtilmiş olmalı).
- **ASLA** binary blob, derlenmiş `.so`/`.dll`/`.dylib` dosyalarını depo dışı kaynaklardan ekleme.
- **ASLA** `replace` direktifi ile depo dışı, kişisel forklara yönlendirme yapma (kullanıcı açıkça istemedikçe).
- **ASLA** kimlik bilgisi, token, anahtar depola — `.env` ve benzeri dosyalar `.gitignore`'da olmalı.

### 1.3. Tercih edilen kütüphaneler

| Amaç | Tercih | Neden |
|---|---|---|
| Desktop framework | `github.com/wailsapp/wails/v2` | Resmi, aktif, native webview kullanır (Electron'a göre küçük binary) |
| System tray | Wails v2 built-in tray yok; `github.com/getlantern/systray` veya energye/systray | Yaygın, test edilmiş, çapraz platform |
| Logger | `log/slog` (standart) | Stdlib, ek bağımlılık yok |
| Konfigürasyon | `github.com/spf13/viper` veya stdlib | Bakımlı, geniş kullanım |
| HTTP client | `net/http` (stdlib) | Ek bağımlılık gerekmez |
| Test | `testing` + `github.com/stretchr/testify` | Stdlib öncelikli |

Bu listede olmayan bir paket eklemen gerekiyorsa **1.1**'i tek tek uygula ve kararı PR/commit açıklamasında belge­le.

### 1.4. Onaylanmış istisnalar (kayıt)

Aşağıdaki bağımlılıklar **1.1** süzgecinden geçirilmiş ve kullanıcı tarafından açıkça onaylanmıştır:

- **`frida` (PyPI, host tarafı)** — Frida Manager'ın enstrümantasyonu sürmek için kullandığı Python paketi. Lisansı **wxWindows Licence (LGPL-2.0-or-later + istisna)**; bu liste dışı olduğundan kullanıcı onayı alınmıştır. **Depoya gömülmez/link edilmez**: tıpkı cihaza pushlanan `frida-server` gibi, çalışma anında kullanıcıya özel bir venv'e kurulur. Kurulum sertleştirmesi: cihazdaki sürümle birebir eşlenik tek wheel seçilir, **SHA256 PyPI'dan doğrulanır**, `pip install --no-index --no-deps --only-binary=:all:` ile çevrimdışı kurulur (pip ağdan çözümleme yapmaz, sdist derlemez). Host yalnızca `files.pythonhosted.org`/`pypi.org` ile sınırlıdır. Alternatif, tamamen lisans-temiz yol: kullanıcı kendi yorumlayıcısını kaydeder (**bring-your-own**), adbq hiçbir şey kurmaz.
- **CodeMirror 6 (npm)** — `codemirror`, `@codemirror/{state,view,language,lang-javascript}`, `@lezer/highlight`. Tümü **MIT**, tek birinci-taraf yayıncı (marijn). Tam sürümle pinlenir, `package-lock.json` SRI hash'leriyle commit edilir, `--ignore-scripts` ile kurulur, `npm audit --omit=dev` temiz olmalı.

- **`rootAVD` (GitLab, host tarafı)** — Play Store system-image'larının ramdisk'ine Magisk kuran üçüncü parti kabuk betiği. **§1.1'in üç kriterini birden karşılamıyor** ve kullanıcı onayıyla istisna olarak kabul edilmiştir:
  - **Lisans GPL-3.0** (liste dışı → onay gerekti),
  - **son commit 2024-10-04** ("son 12 ay" kuralını karşılamıyor),
  - **⭐234** ("500+" eşiğinin altında), sürüm etiketi yok.

  Bu yüzden bağımlılık olarak *alınmaz*, kullanıcının çalıştırdığı harici bir araç olarak *sürülür* — tıpkı `frida-server` gibi. Bağlayıcı kurallar:
  - **Depoya dahil edilmez, link edilmez, `go.mod`/`package.json`'a girmez.** GPL yükümlülüğü adbq'ya bulaşmaz.
  - **Kullanıcı onayı olmadan indirilmez.** Onay diyaloğu kaynağı, commit'i, SHA-256'ları, lisansı ve riskleri gösterir (`RootAVDInfo.Disclosures`).
  - **Sabit commit** indirilir (branch değil). Çalıştırılan tek dosya `rootAVD.sh` ve cihaza ulaşabilen tek ikili `Magisk.zip`, koda gömülü **SHA-256** ile doğrulanır; uyuşmazlık **tüm indirmeyi siler**.
  - İzinli tek indirme host'u **`gitlab.com`**. Arşiv açılırken yol kaçışı tüm arşivi reddettirir.
  - Hedef `<UserCacheDir>/adbq/rootavd/<commit>/` — atılabilir, kullanıcı tek tıkla siler.
  - **adbq'nun doğrulayamadığı** bir şey varsa (rootAVD'nin GitHub'dan kendi indirdiği Magisk sürümü) bu **gizlenmez**, onay metninde yazılır.

  Detay ve gerekçe: [`docs/emulator-manager.md`](docs/emulator-manager.md) §8.

- **`jadx` (GitHub, host tarafı)** — APK/DEX dekompiler'ı. **§1.1'i karşılıyor**: lisans **Apache-2.0** (liste içi), aktif bakımlı, ⭐40k+, sürüm etiketli release'ler. Yani bir istisna değil — **kayıt**: bağımlılık olarak alınmadığı, kullanıcının indirdiği harici bir araç olarak sürüldüğü için burada belgelenir. Bağlayıcı kurallar:
  - **Depoya dahil edilmez, link edilmez, `go.mod`/`package.json`'a girmez.**
  - **Kullanıcı onayı olmadan indirilmez.** Onay diyaloğu sürümü, kaynağı, SHA-256'yı, lisansı ve Java gereksinimini gösterir (`JadxInfo.Disclosures`).
  - **Sabit sürüm** indirilir; arşivin **SHA-256'sı koda gömülüdür**, uyuşmazlık **tüm indirmeyi siler**.
  - İzinli indirme host'ları **`github.com`** ve **`objects.githubusercontent.com`**. Arşiv açılırken yol kaçışı tüm arşivi reddettirir.
  - **Elle güncelleme**: kullanıcı ya kendi kurulumunu gösterir, ya "yeni sürüm var mı" der — o zaman sürüm ve GitHub'ın yayınladığı asset `digest`'i onayda gösterilir ve indirme ona karşı doğrulanır. **Digest yayınlanmamışsa kurulmaz.** adbq kendi kendini güncellemez.
  - Hedef `<UserCacheDir>/adbq/jadx/<sürüm>/` — atılabilir, kullanıcı tek tıkla siler.
  - **JRE indirilmez.** Java yalnızca bu bilgisayarda bulunanlar arasından çözümlenir; yoksa ne bulunamadığı söylenir.

  Detay ve gerekçe: [`docs/apk-analysis.md`](docs/apk-analysis.md).

Yeni bir Frida bağımlılığı/kaynağı eklerken: indirilen her şey host-allowlist + SHA256 ile doğrulanmalı; CodeShare kaynağı **güvenilmez veridir** (önce gösterilir, indirildiğinde çalıştırılmaz). Detay: [`docs/frida-manager.md`](docs/frida-manager.md).

## 2. Kod Kalitesi Kuralları

Detaylar için: [`docs/development-guidelines.md`](docs/development-guidelines.md).

Özet:

- **Go**: `gofmt`, `goimports`, `go vet ./...`, `staticcheck ./...` temiz olmalı.
- **Frontend**: `eslint` + `prettier` temiz olmalı. TypeScript strict mode açık.
- **Commit öncesi**: `wails doctor`, build, test, lint geçmeli.
- **Hata yönetimi**: Hatalar yutulmaz; `error` döndüren çağrılar açıkça ele alınır.
- **Yorum**: Yorumu sadece *neden*'i açıklamak için yaz; *ne* yaptığını isimlerden anlat.
- **Sırlar**: Kaynak kodda hardcoded sır yok. Yapılandırma `.env.local` veya OS keychain'den okunur.

## 3. Wails v2 Spesifik

Detaylı referans: [`docs/wails-v2-reference.md`](docs/wails-v2-reference.md).

Tepsi/menü bar / arkaplana gizleme: [`docs/tray-and-background.md`](docs/tray-and-background.md).

Kısa kurallar:

- Wails CLI **sadece** resmi yolla ve **pinli sürümle** (CI ile aynı) kurulur:
  `go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0`
  (`@latest` kullanma — pinsiz kurulum, ele geçmiş bir release'i çalıştırma riskidir.)
- Yeni proje açarken yalnızca resmi şablonlar (`vanilla`, `react-ts`, `svelte-ts`, `vue-ts`) kullanılır.
- `wails.json`, `go.mod`, `frontend/package.json` değişikliklerinin gerekçesi commit mesajında.
- macOS için `mac.Options{}`, Windows için `windows.Options{}`, Linux için `linux.Options{}` ayrı ayrı yapılandırılır — tek bir blok ile platformlar harmanlanmaz.

## 4. Çalışma Akışı

1. Yeni bir özellik veya değişiklik talebi geldiğinde:
   - Önce ilgili dokümanı (`docs/`) oku.
   - Etkilenecek dosyaları listele.
   - Yeni bağımlılık gerekiyorsa **1.1**'i tamamla.
2. Geliştirirken küçük commit'ler at; her commit yeşil build üretmeli.
3. UI değişikliklerinden sonra `wails dev` ile manuel olarak doğrula — sadece type-check / test yeterli **değildir**.
4. PR/commit mesajı:
   - "ne" değil **"neden"** üstüne odaklı,
   - varsa kırıcı değişiklikleri açıkça belirten,
   - eklenmiş bağımlılığın gerekçesini içeren.

## 4.1. Komut şeffaflığı (bağlayıcı)

adbq bir adb sarmalayıcısıdır; kullanıcı bir düğmeye bastığında cihazda bir
`adb …` komutu çalışır. **Bu komut kullanıcıdan gizlenmez.**

- Cihazda etki yaratan **her** UI eylemi, çalıştıracağı komutu gösterebilmelidir.
- Komut metnini **backend üretir** (`Commands []string` / `…Plan` / `StepPreview`);
  frontend'de elle string birleştirilmez — yanlış komut göstermek, hiç
  göstermemekten kötüdür.
- Komutu üreten fonksiyon **saf ve birim testli** olur: `CommandRenderer` alır,
  komutun yazımını `internal/adb/command_text.go` kararlaştırır
  (`DeviceCommandText` / `ShellCommandText` / `HostCommandText`).
- Eylemin çalıştırdığı uzak komut, önizlemenin okuduğu **aynı fonksiyondan**
  gelir (ör. `appClearRemote`, `rmRemote`, `iptFlushCmd`, `fridaStartRemote`) —
  aksi hâlde önizleme, eylemin bir anlatımı olur ve zamanla ondan ayrışır.
- Yıkıcı/geri alınamaz eylemlerde komut **onay diyaloğunda** görünür.
- Akan işlemlerde (capture, logcat, frida-server) komut panelde **canlı** durur.
- Metin terminale yapıştırıldığında çalışmalıdır; sır/token içermez.
- Gösterim tek bir kontrolden geçer: `CommandPreview` (kapanır/açılır, tamamını
  kopyalar). Frontend elle komut kurmaz; backend cevap verene kadar gösterilecek
  komut yoktur.

Yeni bir cihaz eylemi, komut gösterimi olmadan tamamlanmış sayılmaz.
Desen, envanter ve geriye dönük fazlı plan: [`docs/command-visibility.md`](docs/command-visibility.md).

## 5. Asla yapma listesi

- Kullanıcıya sormadan **yıkıcı** komut (`rm -rf`, `git reset --hard`, `git push --force`, drop table, vb.) çalıştırma.
- Sırları, anahtarları commit etme.
- `wails doctor` hatalarını "ignore" ederek devam etme.
- Üretim build'ini test etmeden release yapma.
- Doğrulanmamış kaynaklardan paket çekme.
