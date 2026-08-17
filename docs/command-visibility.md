# adbq — Komut Şeffaflığı: kural, desen ve geriye dönük yol haritası

adbq bir adb sarmalayıcısıdır. Kullanıcı bir düğmeye bastığında cihazda
çalışan şey bir `adb …` komutudur. **Bu komut kullanıcıdan gizlenmez.**

Amaç üç yönlü:

1. **Güven** — kullanıcı cihazına ne yapıldığını görür; kör bir kutuya
   güvenmek zorunda kalmaz.
2. **Öğreticilik** — adbq'yu kullanan kişi aynı işi terminalde de yapabilir
   hale gelir.
3. **Hata ayıklama** — bir işlem başarısız olduğunda "hangi komut, hangi
   çıktı" sorusu tek tıkla yanıtlanır; hata raporu kopyalanabilir olur.

---

## 1. Kural (bağlayıcı)

> Cihaz üzerinde bir etki yaratan **her** UI eylemi, çalıştıracağı komutu
> kullanıcıya gösterebilmelidir.

Uygulama kuralları:

- **K1 — Komutun kaynağı backend'dir.** Gösterilen metin, frontend'de elle
  yeniden yazılmaz; Go tarafı komutu üretir ve bir alanla (`commands []string`
  ya da `StepPreview`) döndürür. Aksi hâlde gösterim ile gerçek zamanla
  birbirinden ayrışır — yanlış komut göstermek, hiç göstermemekten kötüdür.
- **K2 — Yıkıcı/geri alınamaz eylemlerde komut, onay diyaloğunda görünür.**
  (kurulum, kaldırma, flush, remount, reboot, kural silme, dosya silme…)
  Kullanıcı "Evet" demeden önce ne çalışacağını okur.
- **K3 — Sürekli/akan işlemlerde komut panelde görünür.** (capture, logcat,
  frida-server, scrcpy) Metin, o an geçerli parametrelerle **canlı** güncellenir.
- **K4 — Çok adımlı işlemlerde tüm adımlar listelenir**, tek satır değil.
  Örnek: `.apks` dışa aktarımı `pm path` + N adet `pull` + paketleme.
- **K5 — Salt-okunur listeleme çağrıları için zorunlu değildir**, ama tercih
  edilir (ör. "Yenile" düğmesinin ipucunda komut).
- **K6 — Gizli veri sızdırma.** Komut metni cihaz seri numarası içerir; token,
  parola, sertifika özel anahtarı **içermez** — bunlar `<redacted>` ile
  gösterilir.
- **K7 — Kopyalanabilir olmalı.** `CodeBlock` bileşeni tıkla-kopyala sağlar;
  gösterilen metin terminale yapıştırıldığında **çalışır** olmalıdır (host
  yolları tırnaklanır, cihaz tarafı komutlar `adb -s <serial> shell '…'`
  biçiminde tam yazılır).

## 2. Standart desen

**Frontend** (`frontend/src/ui.tsx: CodeBlock`):

```tsx
<div style={{marginTop: 10, fontSize: 11}}>
  <span className='muted'>Underlying command (click to copy):</span>{' '}
  <CodeBlock multiline>{plan.commands.join('\n')}</CodeBlock>
</div>
```

**Backend** — eylemi yapan fonksiyonun yanında, aynı girdiyle komutu üreten
**saf** bir eş fonksiyon bulunur; bu eş fonksiyon birim testlidir:

```go
// ApkSet.Commands — ApkSetOf tarafından doldurulur, apkSetFromPaths saf ve testli.
type ApkSet struct {
    …
    Commands []string `json:"commands"`
}
```

Onay gerektiren eylemlerde ayrıca bir **plan** tipi olur: ne yapılacağını,
neyin atlandığını ve komutu birlikte döndürür (`ApkInstallPlan`,
`StepPreview`, `TcpdumpAutoPlan` aynı fikrin örnekleri).

Referans uygulamalar:

| Yer | Ne gösteriyor |
|---|---|
| `screens/Network.tsx` (proxy) | `settings put global http_proxy …` — metin `adb.ProxyCommand`'dan gelir |
| `screens/Network.tsx` (capture) | tam `tcpdump` komut satırı, parametreler değiştikçe canlı |
| `screens/Apps.tsx` (APK bölümü) | `pm path` + `pull` listesi; `JAVA_HOME=… jadx-gui <girdiler>`; binary toplama; kurulum onayında `install-multiple` |
| `screens/Emulators.tsx` (sil / rootAVD) | onay diyaloğunun içinde, backend'den gelen komut |

### 2.1. Gösterim: tek bir kontrol

Komut nerede görünüyorsa `CommandPreview` (`frontend/src/ui.tsx`) ile görünür:

- **kapalıyken tek satır** — ilk komut ve adım sayısı; tıklayınca açılır,
- `copy` düğmesi her hâlde **tamamını** kopyalar,
- `defaultOpen`, komutun gözde durmasını gerektiren yerler için: akan işlemler
  (K5) ve yıkıcı eylemlerin onay diyalogları (K4).

Kural komutun **ulaşılabilir** olması; her zaman ekranı kaplaması değil. Bir
panelde üç eylem varsa üçünün listesi birden açıkken panel okunmuyor — bu da
kullanıcının komut panellerini görmezden gelmesiyle sonuçlanıyor.

Aynı sebeple frontend'de **elle komut kurulmaz**: backend cevap verene kadar
gösterilecek komut yoktur (`CommandPreview` boş listede hiç render etmez).
Yaklaşık bir satır göstermek, yanlış komut göstermenin yoludur.

---

## 3. Geriye dönük durum envanteri

Durum: ✅ var · ◐ kısmi (bazı eylemlerde) · ✗ yok

| Ekran | Eylemler | Durum |
|---|---|---|
| Network — proxy | `settings put global http_proxy` | ✅ |
| Network — capture | `tcpdump` başlat/durdur/pull | ✅ |
| Apps — APK dışa aktar / kur | `pm path`, `pull`, `install-multiple` | ✅ |
| Apps — jadx ile aç | `pm path`, `pull`, `jadx-gui <girdiler>` | ✅ |
| Apps — binary indir | `pm path`, `pull`, `pull …/lib` | ✅ |
| Emulators — başlat/durdur | `emulator -avd …`, `adb -s … emu kill` | ✅ |
| Emulators — AVD oluştur/sil | `avdmanager create/delete avd` | ✅ |
| Emulators — donanım düzenle | yazılacak `config.ini` anahtarları | ✅ |
| Emulators — system image | `sdkmanager <pkg>` / `--uninstall` | ✅ |
| Emulators — rootAVD | `bash rootAVD.sh <ramdisk> [restore]` | ✅ |
| Network — CA sertifikası | remount, `cp`, hash, reboot | ✗ |
| Network — hosts | remount, `cat >`, DNS flush | ✗ |
| Network — DNS/bağlantılar | `ss`, `getprop`, `ndc resolver flushnet` | ✗ |
| Apps — kaldır/temizle/durdur/başlat | `pm uninstall`, `pm clear`, `am force-stop`, `monkey` | ✗ |
| Apps — uygulama verisi dışa aktar | `tar -czf … && pull` | ✗ |
| Files | `ls -l`, `rm`, `mkdir`, `push`, `pull` | ✗ |
| Forwards | `adb forward/reverse --list/--remove` | ✗ |
| Iptables | `iptables …` / `nft …` (Undo/Export dahil) | ◐ (kural metni var, tam komut yok) |
| Frida — sunucu | `chmod`, başlatma satırı, `pkill`, log `cat` | ◐ (log paneli var) |
| Frida — kurulum | push, `chmod`, arşiv açma | ✗ |
| Frida — oturum | host tarafı `frida` çağrısı, `adb forward` | ✗ |
| Logcat | `logcat -v … --pid=…` | ✗ |
| Processes | `ps -A` / procfs okuma | ✗ |
| Overview | `reboot`, `tcpip`, `screenrecord`, `screencap`, `scrcpy` | ✗ |
| Profiles | uygulanacak adımların komutları | ◐ (`StepPreview` var, komut metni yok) |
| Capture (live) | `tcpdump` + akış | ◐ (Network'te var, burada yok) |
| Shell | — (kullanıcı zaten komutu yazıyor) | yok sayılır |

## 4. Fazlı yol haritası

Her faz kendi commit'i, yeşil build, `wails dev` ile görsel doğrulama.
Sıra "yıkıcılık + kullanıcı kafa karışıklığı" önceliğine göre.

### Faz A — Yıkıcı eylemler (en yüksek öncelik)
Onay diyaloğuna komut eklenecek eylemler: `pm uninstall`, `pm clear`,
`am force-stop`, Files `rm -r`, Iptables `flush` / policy / chain silme,
`reboot`, CA sertifikası kurulumu (remount içerir), hosts yazımı.
Backend: her biri için `…Plan()` ya da `…Command()` saf fonksiyonu + birim testi.

### Faz B — Uzun süren / akan işlemler
Logcat, Processes, Capture (live), Frida sunucu başlatma, scrcpy, screenrecord.
Panelde canlı komut metni; parametre değiştikçe güncellenir.
Frida sunucu satırı zaten `frida.go` içinde üretiliyor — sadece
dışa açılması gerekiyor.

### Faz C — Dosya ve ağ yardımcıları
Files (`ls`/`push`/`pull`/`mkdir`), Forwards, DNS flush, bağlantı listeleme,
uygulama verisi dışa aktarma.

### Faz D — Profiller ve toplu işlemler
`StepPreview`'a `command` alanı; profil uygulama önizlemesi her adımın
komutunu gösterir. Iptables import/undo aynı mekanizmayı kullanır.

### Faz E — Genel altyapı
- `RunCommand`/`RunCommandRoot` üzerinden geçen her çağrının **son çalıştırılan
  komut** kaydı: cihaz başına dönen tampon, "Son komutlar" paneli
  (mevcut `scrollback.go` deseni izlenir).
- Hata toast'larında "komutu kopyala" eylemi.
- `docs/development-guidelines.md`'ye kod incelemesi maddesi: yeni bir cihaz
  eylemi eklendiğinde komut gösterimi olmadan PR birleştirilmez.

## 5. Yeni özellik eklerken kontrol listesi

- [ ] Backend komutu üretiyor mu (frontend'de string birleştirme yok)?
- [ ] Komut üreten fonksiyon saf ve birim testli mi?
- [ ] Yıkıcıysa onay diyaloğunda komut görünüyor mu?
- [ ] Metin terminale yapıştırıldığında çalışıyor mu (tırnaklama, `-s <serial>`)?
- [ ] Sır/token içermiyor mu?
- [ ] Çok adımlıysa adımların tamamı listeleniyor mu?
