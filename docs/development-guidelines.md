# Geliştirme Yönergeleri

Bu doküman; kod yazım stili, test yaklaşımı, hata yönetimi, performans ve sürüm kuralları için referanstır. CLAUDE.md ile birlikte okunmalıdır.

## 1. Repository Yapısı (önerilen)

```
adbq/
├── CLAUDE.md
├── README.md
├── wails.json
├── go.mod
├── go.sum
├── main.go                  # Wails giriş noktası
├── app.go                   # App struct, ctx tutucu, OnStartup vb.
├── internal/
│   ├── tray/                # Sistem tepsisi mantığı
│   ├── config/              # Yapılandırma okuma/yazma
│   ├── store/               # Yerel veri (SQLite, dosya, vs.)
│   └── platform/            # macOS/Windows/Linux'a özgü kod
├── frontend/                # Wails frontend (React/Svelte/Vue)
│   ├── package.json
│   ├── src/
│   └── dist/                # build çıktısı, .gitignore'da
├── build/                   # Wails ürettiği build asset'leri
└── docs/
    ├── development-guidelines.md
    ├── wails-v2-reference.md
    └── tray-and-background.md
```

## 2. Go Tarafı

### 2.1. Stil

- `gofmt -s` + `goimports` zorunlu.
- Paket adları kısa, küçük harf, alt çizgisiz.
- Exported API'lerin tamamı doc comment'lı (`// FuncName ...`).
- Anlamsız yorum yazma; *neden* yaz, *ne* yazma.

### 2.2. Statik analiz

Her commit öncesi:

```bash
go vet ./...
staticcheck ./...
govulncheck ./...
```

`staticcheck` `golang.org/x/tools/cmd/staticcheck`, `govulncheck` ise `golang.org/x/vuln/cmd/govulncheck` ile kurulur.

### 2.3. Hata Yönetimi

- Hata yutmak yasak: `_ = doSomething()` yerine en azından log + dönüş.
- `errors.Is` / `errors.As` ile tip ayırt et.
- Sentinel hatalar `var ErrXxx = errors.New(...)` formunda; sarmalama için `fmt.Errorf("... : %w", err)`.
- `panic`'i yalnızca kurtarılamaz başlatma (ör. embed asset bulunamadı) için kullan.

### 2.4. Bağlam (`context.Context`)

- Tüm uzun süreli ya da iptal edilebilir çağrılarda `ctx` ilk parametre.
- Wails `OnStartup(ctx context.Context)` ile uygulamanın ana ctx'ini verir; long-running goroutine'leri bu ctx'in `Done()` kanalı ile durdur.

### 2.5. Concurrency

- Goroutine başlattıysan **kim durduracak**, **kim hata raporlayacak** soruları net olsun.
- Channel kapatma yönü tek olmalı (yazan kapatır).
- Paylaşılan state için `sync.Mutex`/`sync.RWMutex` veya channel mesajlaşması; karışım önerilmez.

### 2.6. Test

- Birim test: `_test.go`, table-driven.
- `testify/require` setup hatalarında, `testify/assert` ana assert'lerde.
- Race detector ile çalıştır: `go test -race ./...`.
- UI/Wails katmanı integration test'i için: build edilebilirliği CI'da kontrol et; davranış testi manuel.

## 3. Frontend Tarafı

### 3.1. Stil

- **TypeScript strict**: `tsconfig.json`'da `strict: true`, `noUncheckedIndexedAccess: true`.
- `eslint` + `@typescript-eslint`, `prettier` ile format.
- React kullanılırsa: Function component + hooks, class component yasak.

### 3.2. Wails Bridge

- Go fonksiyonları `frontend/wailsjs/go/...` altına otomatik üretilir; bu dosyalar **el ile düzenlenmez** ve commit'lenir ki frontend tipleri çalışsın.
- Frontend'de `import { FonksiyonAdi } from "../wailsjs/go/main/App"` şeklinde tüketilir.

### 3.3. Performans

- Wails native webview kullanır; ana iş parçacığını JS ile bloke etme.
- Büyük listelerde virtualization (`react-window` vb. ama önce gerçek darboğaz olduğunda).

## 4. Çapraz Platform Notları

- **macOS**: Universal binary için `wails build -platform darwin/universal`. Code signing & notarization release pipeline'da yapılır.
- **Windows**: WebView2 runtime hedef makinede gerekli. Installer (NSIS/MSI) `wails build -nsis` ile üretilir.
- **Linux**: `libgtk-3-dev`, `libwebkit2gtk-4.0-dev` (veya 4.1) gerekir. AppImage / `.deb` paketleme ayrıca yapılır.

## 5. Sürümleme & Commit

- Semantic versioning: `MAJOR.MINOR.PATCH`.
- Commit konvansiyonu:
  - `feat(scope): ...` yeni özellik
  - `fix(scope): ...` bug fix
  - `refactor(scope): ...`
  - `docs(scope): ...`
  - `chore(scope): ...`
  - `build(scope): ...`
- Kırıcı değişiklik: `feat!: ...` veya body içinde `BREAKING CHANGE:` satırı.

## 6. CI Önerileri

Asgari kontroller:

1. `go vet ./...`
2. `staticcheck ./...`
3. `go test -race ./...`
4. `govulncheck ./...`
5. `cd frontend && npm ci && npm run lint && npm run typecheck && npm run build`
6. `wails build -platform <hedef>` (release branch'lerde)

## 7. Sırlar & Yapılandırma

- `.env.local`, `*.pem`, `*.key`, `config.local.*` → `.gitignore`.
- Runtime sırları:
  - macOS: Keychain (`github.com/keybase/go-keychain` veya `99designs/keyring`).
  - Windows: Credential Manager (`zalando/go-keyring`).
  - Linux: Secret Service.
- Üç platforma tek API ile erişmek için: `github.com/zalando/go-keyring` (bakımlı, küçük, lisans uyumlu).

## 8. Logger

`log/slog` örneği:

```go
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))
slog.SetDefault(logger)
slog.Info("uygulama başladı", "version", buildVersion)
```

Wails `logger.Logger` arayüzü için adapter yazılabilir (bkz. `pkg/logger`).
