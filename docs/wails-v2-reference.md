# Wails v2 — Yerel Referans

Bu doküman, Wails v2 ile çalışırken sık ihtiyaç duyulan konuların özetidir. Resmi dokümantasyon: https://wails.io/docs (referans için; doğrulama her zaman güncel resmi siteden yapılır).

## 1. Wails Nedir?

Wails, Go backend'i ile web tabanlı bir frontend'i tek bir binary'de paketlemeye yarayan masaüstü framework'üdür. Electron'un aksine **bundled Chromium taşımaz**; her platformun **native webview**'unu kullanır:

- **Windows** → WebView2 (Edge / Chromium)
- **macOS** → WKWebView
- **Linux** → WebKit2GTK

Binary'ler bu sayede genelde 5–15 MB arasındadır.

## 2. Önkoşullar

| Platform | Gereksinim |
|---|---|
| Tümü | Go 1.18+ (embed pattern desteği için), Node 15+ ve npm |
| Windows | WebView2 Runtime (Win11'de yerleşik, Win10'a manuel) |
| macOS | Xcode Command Line Tools (`xcode-select --install`) |
| Linux | `build-essential`, `libgtk-3-dev`, `libwebkit2gtk-4.0-dev` (veya 4.1) |

Doğrulama:

```bash
wails doctor
```

Bu komut eksik bağımlılıkları listeler ve nasıl kuracağını söyler. Yeşil değilse devam etme.

## 3. Kurulum

```bash
# Wails CLI — pinli sürüm (CI ile aynı; @latest pinsizdir, kullanma)
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0

# PATH kontrolü (zsh)
echo 'export PATH="$HOME/go/bin:$PATH"' >> ~/.zshrc
```

Yeni proje:

```bash
wails init -n adbq -t react-ts
# veya: vanilla, svelte-ts, vue-ts, lit-ts, preact-ts
```

## 4. Proje Yapısı (init sonrası)

```
adbq/
├── app.go              # App struct & ctx
├── main.go             # wails.Run(...) çağrısı
├── wails.json          # Wails CLI metadata
├── go.mod
├── build/
│   ├── appicon.png
│   ├── darwin/         # Info.plist, entitlements
│   └── windows/        # manifest, installer assets
└── frontend/
    ├── package.json
    ├── src/
    └── wailsjs/        # Otomatik üretilen JS/TS köprüsü
```

## 5. Temel `main.go` İskeleti

```go
package main

import (
    "embed"

    "github.com/wailsapp/wails/v2"
    "github.com/wailsapp/wails/v2/pkg/options"
    "github.com/wailsapp/wails/v2/pkg/options/assetserver"
    "github.com/wailsapp/wails/v2/pkg/options/mac"
    "github.com/wailsapp/wails/v2/pkg/options/windows"
    "github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
    app := NewApp()

    err := wails.Run(&options.App{
        Title:             "adbq",
        Width:             1024,
        Height:            768,
        MinWidth:          640,
        MinHeight:         480,
        DisableResize:     false,
        Fullscreen:        false,
        Frameless:         false,
        StartHidden:       false,
        HideWindowOnClose: true,   // X butonu uygulamayı kapatmaz, gizler
        AlwaysOnTop:       false,

        AssetServer: &assetserver.Options{
            Assets: assets,
        },
        BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},

        OnStartup:     app.startup,
        OnDomReady:    app.domReady,
        OnBeforeClose: app.beforeClose,
        OnShutdown:    app.shutdown,

        Bind: []interface{}{ app },

        Mac: &mac.Options{
            TitleBar: mac.TitleBarHiddenInset(),
            About: &mac.AboutInfo{
                Title:   "adbq",
                Message: "© 2026 adbq",
            },
            // ActivationPolicy: mac.NSApplicationActivationPolicyAccessory,
            // → menubar-only app yapmak için (dock'ta görünmez)
        },
        Windows: &windows.Options{
            WebviewIsTransparent:              false,
            WindowIsTranslucent:               false,
            DisableWindowIcon:                 false,
        },
        Linux: &linux.Options{
            WindowIsTranslucent: false,
            WebviewGpuPolicy:    linux.WebviewGpuPolicyAlways,
        },
    })
    if err != nil {
        println("Error:", err.Error())
    }
}
```

## 6. `app.go` Çekirdek Yaşam Döngüsü

```go
package main

import (
    "context"

    "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
    ctx context.Context
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
    // background goroutine'ler burada başlatılır
}

func (a *App) domReady(ctx context.Context) {
    // frontend DOM hazır
}

// X butonuna basıldığında çağrılır.
// true döndürürsen kapanma iptal edilir.
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
    runtime.WindowHide(ctx)
    return true
}

func (a *App) shutdown(ctx context.Context) {
    // kaynak temizliği
}

// Frontend'den çağrılabilir.
func (a *App) Greet(name string) string {
    return "Merhaba " + name
}
```

`HideWindowOnClose: true` ve `OnBeforeClose` aynı anda kullanılmaz. Tepsi entegrasyonu yapacaksan **`OnBeforeClose` ile elle `WindowHide`** demek daha kontrollü; sırf "X gizlesin" istiyorsan `HideWindowOnClose: true` yeterli.

## 7. Sık Kullanılan `options.App` Alanları

| Alan | Anlam |
|---|---|
| `Title` | Pencere başlığı |
| `Width`, `Height`, `MinWidth`, `MaxWidth`, vb. | Pencere boyutları |
| `DisableResize` | Boyutlandırmayı kapat |
| `Fullscreen` | Açılışta tam ekran |
| `Frameless` | Çerçevesiz pencere (özel title bar yazacaksan) |
| `StartHidden` | Pencereyi gizli aç (tray-only başlatma için) |
| `HideWindowOnClose` | X = gizle, quit değil |
| `AlwaysOnTop` | Her zaman üstte |
| `BackgroundColour` | Webview arkası |
| `Bind` | Frontend'e expose edilecek Go struct'ları |
| `EnumBind` | Frontend'e expose edilecek enum türleri |
| `Menu` | Uygulama menüsü (`menu.NewMenu()`) |

Platform spesifik alanlar `Mac`, `Windows`, `Linux` içine girer (bkz. bölüm 8).

## 8. Platform Spesifik Seçenekler

### 8.1. `mac.Options`

```go
Mac: &mac.Options{
    TitleBar: mac.TitleBarHiddenInset(), // veya .TitleBarDefault(), .TitleBarHidden()
    Appearance: mac.NSAppearanceNameDarkAqua,
    WebviewIsTransparent: false,
    WindowIsTranslucent:  false,
    About: &mac.AboutInfo{
        Title:   "adbq",
        Message: "Geliştirici aracı",
        Icon:    iconBytes, // []byte, build/appicon.png'den okunur
    },
    // ActivationPolicy:
    //   mac.NSApplicationActivationPolicyRegular   (default, dock'ta görünür)
    //   mac.NSApplicationActivationPolicyAccessory (menubar app, dock yok)
    //   mac.NSApplicationActivationPolicyProhibited
}
```

> **Not**: `ActivationPolicy` alanı Wails 2.8+ ile geldi. Eski sürümlerde `build/darwin/Info.plist` içine `LSUIElement = true` ekleyerek aynı sonucu (dock'tan saklama) elde edebilirsin.

### 8.2. `windows.Options`

```go
Windows: &windows.Options{
    WebviewIsTransparent: false,
    WindowIsTranslucent:  false,
    DisableWindowIcon:    false,
    Theme:                windows.SystemDefault, // veya .Dark / .Light
    BackdropType:         windows.Mica,           // Win11 mica/acrylic
    DisableFramelessWindowDecorations: false,
    WebviewUserDataPath: "", // boşsa default
    WebviewBrowserPath:  "", // özel WebView2 ise
}
```

### 8.3. `linux.Options`

```go
Linux: &linux.Options{
    Icon:                iconBytes,
    WindowIsTranslucent: false,
    WebviewGpuPolicy:    linux.WebviewGpuPolicyAlways,
    ProgramName:         "adbq",
}
```

## 9. Frontend ↔ Backend İletişimi

### 9.1. Metot çağırma

```ts
// frontend/src/...
import { Greet } from "../wailsjs/go/main/App";

const msg = await Greet("Ahmet");
```

`Bind: []interface{}{ app }` listesindeki **exported** metotlar otomatik olarak `wailsjs/go/<pkg>/<Struct>.ts` dosyasına yazılır.

### 9.2. Event

```go
// backend
runtime.EventsEmit(a.ctx, "log:line", "yeni satır geldi")
```

```ts
// frontend
import { EventsOn } from "../wailsjs/runtime/runtime";
EventsOn("log:line", (line) => console.log(line));
```

### 9.3. Dialog

```go
selection, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
    Title: "Klasör seç",
})
```

`OpenFileDialog`, `SaveFileDialog`, `MessageDialog` de mevcut.

## 10. Build & Dev

```bash
wails dev                     # canlı yeniden yükleme + frontend dev server
wails build                   # üretim build (geçerli platform)
wails build -platform windows/amd64
wails build -platform darwin/universal
wails build -nsis             # Windows için NSIS installer
wails build -webview2 embed   # WebView2 bootstrapper'ı binary'ye göm
```

`wails build` çıktısı `build/bin/` altındadır.

## 11. Sorun Giderme

| Belirti | Olası neden |
|---|---|
| `wails doctor` "WebView2 not found" | Win10 → https://developer.microsoft.com/microsoft-edge/webview2/ |
| macOS'ta "developer cannot be verified" | Notarization yapılmamış; geliştirme için `xattr -d com.apple.quarantine adbq.app` |
| Linux build hatası `webkit2gtk-4.0` yok | `libwebkit2gtk-4.1-dev` varsa `WAILS_USE_WEBKIT2GTK41=true wails build` |
| Frontend değişiklikleri yansımıyor | `frontend/dist`'i sil, `wails dev` yeniden başlat |
| `bind` ettiğin metot frontend'de yok | Metot adı **PascalCase** ve **exported** olmalı; `wails generate module` ile zorla yenile |
