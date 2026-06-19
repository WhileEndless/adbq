# Sistem Tepsisi, Menü Bar ve Arka Plana Gizleme

Bu doküman, Wails v2 uygulamanın **kapatılmadan** arka planda çalışmasını ve macOS menü bar / Windows system tray / Linux notification area üzerinden yönetilmesini tarif eder.

## 1. Genel Mimari

İki bağımsız problem var, ayrı çözülür:

1. **Tepsi/Menü bar ikonu**: Wails v2'nin **dahili** sistem tepsisi API'si **yoktur**. Bunun için ayrı bir kütüphane çalıştırılır — projede `github.com/getlantern/systray` veya `github.com/energye/systray` kullanılır. Bu kütüphane Wails ile birlikte aynı süreçte çalışabilir.
2. **Pencere yaşam döngüsü**: Wails'in `OnBeforeClose` / `WindowHide` / `WindowShow` API'leri ile pencere kapatma yerine gizleme davranışı yapılır.

```
        ┌────────────────────────┐
        │   Wails App süreci     │
        │  ┌──────────────────┐  │
        │  │ Wails main window│  │ ← runtime.WindowHide/Show
        │  └──────────────────┘  │
        │  ┌──────────────────┐  │
        │  │ systray goroutine│  │ ← tray ikon + menu
        │  └──────────────────┘  │
        └────────────────────────┘
```

## 2. Bağımlılık Ekleme

```bash
go get github.com/getlantern/systray
```

**Linux** ek gereksinim:

```bash
sudo apt install libgtk-3-dev libayatana-appindicator3-dev
```

> Lisans: `getlantern/systray` Apache-2.0 — projemizle uyumlu.

## 3. Pencereyi X ile Kapatınca Gizleme

`options.App`'te:

```go
HideWindowOnClose: true,
```

Bu tek başına çoğu durumda yeter; X'e basılınca pencere `WindowHide` ile gizlenir, uygulama yaşamaya devam eder.

**Ya da** daha programatik kontrol için `OnBeforeClose`:

```go
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
    // "Gerçekten kapat?" diyalogu, vs. burada olabilir
    runtime.WindowHide(ctx)
    return true   // true = kapanmayı engelle
}
```

Aynı anda **ikisini birden kullanma** — `HideWindowOnClose: false` bırakıp tüm kararı `OnBeforeClose`'a vermek daha esnek.

## 4. Açılışta Pencereyi Gösterme/Gizleme

```go
options.App{
    StartHidden: true,   // sadece tray'de başla
    ...
}
```

Sonra tray menüsünden "Show" ile `runtime.WindowShow(ctx)`.

## 5. Tray Mantığı — Cross-Platform

`internal/tray/tray.go`:

```go
package tray

import (
    "context"
    _ "embed"

    "github.com/getlantern/systray"
    "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed icon.ico   // Windows için .ico, macOS için .png template, Linux için .png
var iconData []byte

type Tray struct {
    ctx context.Context
}

func New(ctx context.Context) *Tray { return &Tray{ctx: ctx} }

func (t *Tray) Run() {
    // systray.Run BLOCKING; ayrı goroutine'de çağır
    go systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
    systray.SetIcon(iconData)
    systray.SetTitle("adbq")
    systray.SetTooltip("adbq")

    mShow := systray.AddMenuItem("Göster", "Pencereyi aç")
    mHide := systray.AddMenuItem("Gizle", "Pencereyi gizle")
    systray.AddSeparator()
    mQuit := systray.AddMenuItem("Çıkış", "Uygulamayı kapat")

    go func() {
        for {
            select {
            case <-mShow.ClickedCh:
                runtime.WindowShow(t.ctx)
            case <-mHide.ClickedCh:
                runtime.WindowHide(t.ctx)
            case <-mQuit.ClickedCh:
                runtime.Quit(t.ctx)
                return
            }
        }
    }()
}

func (t *Tray) onExit() {
    // tray kapandığında temizlik
}
```

`app.go` içinde:

```go
import "adbq/internal/tray"

func (a *App) startup(ctx context.Context) {
    a.ctx = ctx
    tray.New(ctx).Run()
}
```

## 6. macOS — Menü Bar Uygulaması (Dock'ta Görünmesin)

macOS'ta tray uygulaması = menü bar uygulaması. Dock'tan saklamak için iki yol:

### 6.1. Wails 2.8+ ile

```go
import "github.com/wailsapp/wails/v2/pkg/options/mac"

Mac: &mac.Options{
    ActivationPolicy: mac.NSApplicationActivationPolicyAccessory,
}
```

### 6.2. Info.plist ile (her sürümde çalışır)

`build/darwin/Info.plist` dosyasına ekle:

```xml
<key>LSUIElement</key>
<true/>
```

Bu hem dock ikonunu kaldırır hem Cmd+Tab'da görünmemeyi sağlar.

### 6.3. Menü bar ikonu — şeffaf template

macOS'ta menü bar ikonu **siyah-şeffaf "template image"** olmalı ki açık/koyu temada otomatik renklensin. PNG'yi şeffaf zeminli, sadece siyah/alfa olarak hazırla. `getlantern/systray`'de macOS için `SetTemplateIcon([]byte, []byte)` veya `SetIcon` ile vereceğin PNG template formatında olmalı.

## 7. Windows — System Tray ikonu

- Format: `.ico` (16x16 ve 32x32 içeren), `//go:embed icon.ico`.
- `WebView2 Runtime` kullanıcı makinesinde değilse uygulama açılmaz — bunu **bootstrapper** ile gömmek için:
  ```bash
  wails build -webview2 embed
  ```
- "Kapat" yerine "Gizle" davranışı: bölüm 3'teki `HideWindowOnClose: true` Windows'ta da aynı şekilde çalışır.

### Görev çubuğunda da görünmesin (yalnızca tray) — ileri seviye

Bu Wails'in `options.Windows` ile doğrudan sağladığı bir alan değil. Çözüm:
- Pencere oluşturulduktan sonra Win32 API ile `WS_EX_TOOLWINDOW` style'ı uygulanır. Bunu güvenli ve kütüphane içinden yapmak için pencere gizliyken bu davranış zaten örtüktür. Görünür pencere için ToolWindow uygulamak istiyorsan `golang.org/x/sys/windows` ile `SetWindowLongPtr(GWL_EXSTYLE, ...)` çağrısı gerekir; bunu yapmak zorunluysa `internal/platform/windows_taskbar.go` altına Windows-only build tag (`//go:build windows`) ile yaz.

## 8. Linux

- GNOME 3.26+ varsayılan olarak system tray'i kaldırdı; **AppIndicator** uzantısı (ör. "AppIndicator and KStatusNotifierItem Support") yüklü olmalı.
- `getlantern/systray` Linux'ta `libayatana-appindicator3` aracılığıyla çalışır.
- "Görev çubuğunda gösterme" Linux'ta WM bağımlıdır; standart bir API yok. Bağımsız davranış için sadece `runtime.WindowHide` yeterlidir.

## 9. Tek Örnek Şartı (Single Instance)

Tray'de zaten çalışan uygulamayı tekrar açmaya çalıştığında ikinci süreç açılmasın istersen:

- macOS: `LSMultipleInstancesProhibited = true` (Info.plist)
- Windows/Linux: Lock file veya named pipe ile sürüm kontrolü. Hazır paket: `github.com/marcsauter/single` (MIT, basit, bakımlı).

```go
s := single.New("adbq")
if err := s.CheckLock(); err != nil {
    // başka süreç var, ona "show" sinyali gönder ve çık
    os.Exit(0)
}
defer s.TryUnlock()
```

## 10. Uçtan Uca Akış (özet)

```
┌──────────────┐  X tıklandı   ┌──────────────┐
│  Main Window │ ────────────► │ OnBeforeClose│
└──────▲───────┘               └──────┬───────┘
       │                              │ WindowHide
       │ WindowShow                   ▼
       │                       ┌──────────────┐
       └─────────────────── tray "Göster" ────┤
                              └──────┬───────┘
                                     │ "Çıkış"
                                     ▼
                              runtime.Quit
```

## 11. Yapma Listesi

- `systray.Run`'ı **ana goroutine** dışında çağırma — bazı platformlarda main-thread kısıtı vardır. `getlantern/systray` bunu kendi başlatma akışında yönetir; örnekte gösterildiği gibi `go systray.Run(...)` ile başlat ve Wails'in event loop'una ana thread'i bırak.
- Tray ikonunu her tıklamada yeniden oluşturma — `onReady` bir kez çalışır.
- macOS'ta normal `.png` koyup "ikonum renkli görünüyor" deme — template image kullan.
- Linux'ta AppIndicator olmadan deneme ve "çalışmıyor" deme; tray paketi sessiz başarısız olabilir, log'a bak.
