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

- Wails CLI **sadece** resmi yolla kurulur:
  `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
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

## 5. Asla yapma listesi

- Kullanıcıya sormadan **yıkıcı** komut (`rm -rf`, `git reset --hard`, `git push --force`, drop table, vb.) çalıştırma.
- Sırları, anahtarları commit etme.
- `wails doctor` hatalarını "ignore" ederek devam etme.
- Üretim build'ini test etmeden release yapma.
- Doğrulanmamış kaynaklardan paket çekme.
