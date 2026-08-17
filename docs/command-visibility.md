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
- **K7 — Kopyalanabilir olmalı.** `CommandPreview` her hâlde tamamını kopyalar;
  gösterilen metin terminale yapıştırıldığında **çalışır** olmalıdır (host
  yolları tırnaklanır, cihaz tarafı komutlar `adb -s <serial> shell '…'`
  biçiminde tam yazılır).

## 2. Standart desen

**Frontend** (`frontend/src/ui.tsx: CommandPreview`):

```tsx
<CommandPreview commands={cmds?.clear ?? []} label='Clear data'/>
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

### 2.0. Komutun yazımı tek yerde kararlaştırılır

`internal/adb/command_text.go`:

| Yardımcı | Ne üretir |
|---|---|
| `DeviceCommandText(serial, args…)` | `adb -s <serial> <args…>` |
| `ShellCommandText(serial, remote)` | `adb -s <serial> shell '<remote>'` — uzak komut **tek argüman** kalır |
| `HostCommandText(bin, args…)` | bu bilgisayarda çalışan komut (emulator, jadx, frida, scrcpy) |
| `quoteArg(s)` | yalnızca kabuğun bozacağı argümanı tırnaklar |
| `CommandRenderer` | `func(remote string, asRoot bool) string` — builder'lar `*Client` yerine bunu alır |

`Client.Renderer(ctx, serial)` cihazın **kabul ettiği** `su` biçimiyle render
eder (`rootWrap`), yani gösterilen satır çalışan satırdır. `PlainRenderer`
cihaza gitmeyen önizlemeler ve testler için `su -c` biçimini kullanır.

Bir builder böyle yazılır: saf fonksiyon + renderer alır, `*Client` üzerinde
aynı adlı ince bir metot cihaz farkındalığını ekler. Örnek:
`AppCommandsFor`, `FileCommandsFor`, `IptablesCommandsFor`, `NetCommandsFor`,
`DeviceCommandsFor`, `FridaCommandsFor`.

**Aynı dizeyi hem çalıştıran hem gösteren tek kaynak.** Eylemin çalıştırdığı
uzak komut, artık kod içinde satır olarak değil bir fonksiyon olarak durur
(`appClearRemote`, `rmRemote`, `iptFlushCmd`, `fridaStartRemote`,
`hostsStrategies`, `cacertStrategies`, `flushDNSScript`…). Önizleme onu okur;
böylece önizleme "eylemin anlatımı" değil, eylemin kendisi olur.

Referans uygulamalar:

| Yer | Ne gösteriyor |
|---|---|
| `screens/Network.tsx` (proxy) | `settings put global http_proxy …` — metin `adb.ProxyCommand`'dan gelir |
| `screens/Network.tsx` (capture) | tam `tcpdump` komut satırı, parametreler değiştikçe canlı |
| `screens/Apps.tsx` (APK bölümü) | `pm path` + `pull` listesi; `JAVA_HOME=… jadx-gui <girdiler>`; binary toplama; kurulum onayında `install-multiple` |
| `screens/Emulators.tsx` (sil / rootAVD) | onay diyaloğunun içinde, backend'den gelen komut |
| `screens/Iptables.tsx` (kural ekle) | tablo + `-A`/`-I N` + `su` biçimi, form değiştikçe backend'den |
| `screens/Files.tsx` (sil) | onay diyaloğunda `rm -rf`, Root anahtarı açıksa `su` sarmalayıcısıyla |
| `screens/Frida.tsx` (oturum) | çalışan sürücü satırı **ve** eşdeğer `frida` CLI çağrısı (etiketli) |

### 2.1. Gösterim: tek bir kontrol

Komut nerede görünüyorsa `CommandPreview` (`frontend/src/ui.tsx`) ile görünür:

- **kapalıyken tek satır** — ilk komut ve adım sayısı; tıklayınca açılır,
- `copy` düğmesi her hâlde **tamamını** kopyalar,
- `defaultOpen`, komutun gözde durmasını gerektiren yerler için: akan işlemler
  (K3) ve yıkıcı eylemlerin onay diyalogları (K2).

Kural komutun **ulaşılabilir** olması; her zaman ekranı kaplaması değil. Bir
panelde üç eylem varsa üçünün listesi birden açıkken panel okunmuyor — bu da
kullanıcının komut panellerini görmezden gelmesiyle sonuçlanıyor.

Aynı sebeple frontend'de **elle komut kurulmaz**: backend cevap verene kadar
gösterilecek komut yoktur (`CommandPreview` boş listede hiç render etmez).
Yaklaşık bir satır göstermek, yanlış komut göstermenin yoludur.

---

## 3. Geriye dönük durum envanteri

Durum: ✅ var · ◐ kısmi (bazı eylemlerde) · ✗ yok — bu tabloda artık ✗ yok.

| Ekran | Eylemler | Durum |
|---|---|---|
| Network — proxy | `settings put global http_proxy` | ✅ |
| Network — capture | `tcpdump` başlat/durdur/pull | ✅ |
| Network — CA sertifikası | push + `chmod`, dört kalıcı strateji, tmpfs overlay | ✅ |
| Network — hosts | staging, beş strateji, md5 doğrulama, Magisk modülü, DNS flush | ✅ |
| Network — DNS/bağlantılar | `ndc resolver …`, `getprop`, `/proc/net/*` | ✅ |
| Apps — APK dışa aktar / kur | `pm path`, `pull`, `install-multiple` | ✅ |
| Apps — jadx ile aç | `pm path`, `pull`, `jadx-gui <girdiler>` | ✅ |
| Apps — binary indir | `pm path`, `pull`, `pull …/lib` | ✅ |
| Apps — kaldır/temizle/durdur/başlat | `pm uninstall`, `pm clear`, `am force-stop`, `monkey` | ✅ |
| Apps — uygulama verisi dışa aktar | root `tar` + `pull` + `rm` | ✅ |
| Files | `ls -lAp`, `rm [-rf]`, `mkdir -p`, `push`+`chmod`/`chown`, `pull` | ✅ |
| Forwards | `forward`/`reverse` ekle, `--remove`, `--list` | ✅ |
| Iptables | `-A`/`-I N`/`-D`/`-F`/`-P`/`-N`/`-X`, save, restore, undo | ✅ |
| Emulators — başlat/durdur | `emulator -avd …`, `adb -s … emu kill` | ✅ |
| Emulators — AVD oluştur/sil | `avdmanager create/delete avd` | ✅ |
| Emulators — donanım düzenle | yazılacak `config.ini` anahtarları | ✅ |
| Emulators — system image | `sdkmanager <pkg>` / `--uninstall` | ✅ |
| Emulators — rootAVD | `bash rootAVD.sh <ramdisk> [restore]` | ✅ |
| Overview — reboot/power off/adbd | `reboot [mode]`, `reboot -p`, `stop/start adbd` | ✅ |
| Overview — Wi-Fi adb / bağlan | `tcpip 5555`, `adb connect <addr>` | ✅ |
| Overview — ekran görüntüsü/kayıt/scrcpy | `exec-out screencap -p`, `screenrecord`+`pull`, `scrcpy` | ✅ |
| Overview — root testi | `which su; ls -d …magisk; magisk -V` | ✅ |
| Logcat | `logcat -v threadtime --pid=… -T … '*:V'`, `logcat -c` | ✅ |
| Processes | `/proc` taraması (root ya da shell — hangisiyse) | ✅ |
| Capture (live) | `exec-out` içinde root'lu `tcpdump -U -s 0 -w -` | ✅ |
| Frida — kurulum | `push` + `chmod 755` | ✅ |
| Frida — sunucu | başlatma satırı (`-D`, yönlendirmeler), procfs `kill`, log `cat` | ✅ |
| Frida — oturum | çalışan sürücü + eşdeğer `frida` CLI çağrısı | ✅ |
| Profiles | her adımın komutları, uygulanma sırasıyla | ✅ |
| Shell | — (kullanıcı zaten komutu yazıyor) | yok sayılır |

Kapsam dışı bırakılanlar — ve nedeni:

- **Salt-okunur listelemelerin çoğu** (uygulama listesi, `dumpsys package`,
  cihaz özellikleri, AVD listesi): K5 zorunlu tutmuyor; her satıra bir komut
  eklemek paneli okunmaz hâle getirir. Etki yaratan eylemlerin hepsi listede.
- **CA sertifikasının tmpfs overlay adımı ve DNS flush betiği** çok satırlı
  betikler. Tek argüman olarak tırnaklanıp aynen gösteriliyorlar (kopyala
  çalışır), ama katlanmış hâlde ilk satırları görünür.
- **Ekran kaydının cihaz yolu** canlı çalışmada zaman damgası taşır; önizleme
  sabit adı gösterir, aksi hâlde satır kopyalanabilir olmaz.
- **jadx / rootAVD / frida-server indirmeleri** komut değil; onay diyaloğunda
  kaynak + SHA-256 + lisans olarak gösterilir (§1.4).

## 4. Fazlı yol haritası — durum

A–D fazları tamamlandı: yıkıcı eylemler onay diyaloğunda komutu gösteriyor,
akan işlemler komutu panelde canlı tutuyor, dosya/ağ yardımcıları ve profil
adımları komutlarını üretiyor. Bunların hepsi backend'de saf, birim testli
builder'lardan geliyor.

Kalan (Faz E — genel altyapı, henüz yapılmadı):

- `RunCommand`/`RunCommandRoot` üzerinden geçen her çağrının **son çalıştırılan
  komut** kaydı: cihaz başına dönen tampon, "Son komutlar" paneli
  (mevcut `scrollback.go` deseni izlenir).
- Hata toast'larında "komutu kopyala" eylemi.
- `docs/development-guidelines.md`'ye kod incelemesi maddesi: yeni bir cihaz
  eylemi eklendiğinde komut gösterimi olmadan PR birleştirilmez.

## 5. Yeni özellik eklerken kontrol listesi

- [ ] Backend komutu üretiyor mu (frontend'de string birleştirme yok)?
- [ ] Komut üreten fonksiyon saf ve birim testli mi (`CommandRenderer` alıyor mu)?
- [ ] Eylemin çalıştırdığı uzak komut, önizlemenin okuduğu **aynı** fonksiyondan mı geliyor?
- [ ] Yıkıcıysa onay diyaloğunda komut görünüyor mu?
- [ ] Metin terminale yapıştırıldığında çalışıyor mu (tırnaklama, `-s <serial>`)?
- [ ] Sır/token içermiyor mu?
- [ ] Çok adımlıysa adımların tamamı listeleniyor mu?
