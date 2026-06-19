# adbq — Eski/Minimal Android Uyumluluk Yol Haritası

Bu belge, eski (API 21–26) ve minimal/stripped ROM'larda (sınırlı toybox/busybox,
AOSP-stil su, SELinux enforcing, eksik binary'ler) tespit edilen uyumluluk
eksiklerini ve bunları gidermek için fazlı planı içerir. 9 modül-bazlı denetim
ajanının bulgularından derlenmiştir (2026-06-19).

Sınıflandırma:
- **FIXABLE-FALLBACK** — özellik çalışabilir, doğru komut/fallback ile düzeltilir.
- **MARK-UNAVAILABLE** — cihazda gerçekten imkansız; UI'da temiz "kullanılamaz" göster.
- **UX-GAP** — çalışıyor ama kafa karıştırıcı / sessiz başarısızlık.

Çekirdek model: `Client.Shell` = `adb shell`; `Client.ShellSU` → `rootWrap` →
`suStyle` (Magisk `su -c` vs AOSP `su -c sh -c`). Bu model 3 kritik eksiği
besliyor: adbd-root (uid0, su binary yok) desteklenmiyor; su arg-form kapsamı dar;
interaktif shell ayrı bir `su root` yolundan gidiyor.

---

## Faz 0 — Temel: Yetenek (Capability) çerçevesi + root modeli  ★ keystone

Diğer fazların çoğu buna dayanır. Tek `Capabilities` struct'ı, serial başına
önbelleğe alınır (yeniden bağlanınca geçersiz), tek batched shell çağrısıyla
toplanır: SDK, SELinux (`getenforce`), RootKind/SuStyle, toolbox lezzeti, binary
varlık taraması (`command -v`).

- **R1/N3/N2** adbd-root (uid=0, `su` binary yok): yeni `suBareRoot` stili —
  shell zaten uid 0 ise `inner`'ı `su` sarmadan çalıştır. hosts/certs/dns flush/
  capture'ı yaygın "rootlu emülatör/userdebug" senaryosunda çalışır kılar.
- **R2** su arg-form kapsamı: `su 0 -c` ve `su 0 sh -c` prob stilleri ekle.
- **R3/S2** interaktif shell: yükseltme komutunu `suStyleFor`'dan al; `id` ile
  `uid=0` doğrula, başarısızsa "user shell'e düştü" durumu göster.
- **O1** ShellSU başarısızlık sınıflandırması: İngilizce substring yerine
  exit-code + pozitif `id`→uid=0 doğrulaması.
- **F1** merkezi `Capabilities(ctx, serial)` + `Device`'a alanları taşı.
- **P1 (kısmi)** `ShellSU` zaten `unavailable bool` döndürüyor; çağıranlar
  kullanmıyor — fallback için kullanılabilir hale getir.

## Faz 1 — "Yüklenebilir ama gösteremiyoruz" (en yüksek görünür değer)

- **P1** Processes: root-only; non-root procfs fallback (ShellSU başarısızsa
  düz `Shell`). **P3** User kolonu hep boş + Cmdline==Name → `/proc/PID/cmdline`
  + `/proc/PID/status` Uid. **P2** hidepid banner'ı (API24+ kısıtlı görünüm).
  **P4** yanıltıcı `top` status metnini düzelt (artık procfs).
- **L1** Logcat parser: opsiyonel yıl + opsiyonel `±HHMM` UTC offset; PID/TID'i
  pozisyonel bul. **L3** `--pid` API<24'te stream'i öldürüyor → SDK'ya göre
  gate + client-side PID filtresi; pkg çalışmıyorsa "bekleniyor" durumu.
  **L7** açık `*:V` filterspec. **L10** stderr'i yakala (tüm sessiz hataları
  görünür kılar). **L4/L5** continuation/banner/tag-split sağlamlaştır.
  **L6** pause satırları düşürüyor → tampona al.
- **N1** Network IP/MAC/iface: `ip`-only → `ifconfig`/`netcfg`/`/proc/net/*`
  fallback (API21–23 boş Overview'i onarır). **H2** DNS lookup ping izin hatasını
  yanlış etiketliyor / sahte "offline".
- **F1/F2** Files: `ls -lAp` → düşürülmüş `ls -l` retry + `MMM DD` tarih anchor
  (API21–22 toolbox'ı onarır). **F3** permission-denied → tipli "root gerekli".

## Faz 2 — Firewall + capture doğruluğu

- **IPT-3** iptables her root çağrısını `rootWrap`'tan geçir (AOSP su onarımı).
  **IPT-4** `head -1` bağımlılığını kaldır (53de802 purge'ü atlamış).
  **IPT-1** nft-only ROM'lar için salt-okunur `nft list ruleset` fallback +
  ayrı parser; mutasyon kapalı kalır. **IPT-2/IPT-5** root-yok vs binary-yok'u
  ayır; 4 temiz durum; sahte "List failed" toast'ını kaldır.
- **C1** capture auto-install manifest'ine x86/x86_64 ekle (emülatörler).
  **C3** `tcpdump -D` boşsa kör `-i any` yerine procfs'ten iface seç.
  **C4/D1** DNS sniffer tcpdump'ı `rootWrap`'tan geçir. **C7** `head -1` kaldır.
  **C2** root gerekli mesajını inline göster.

## Faz 3 — Apps/files/medya doğruluğu

- **SS-1** Screenshot: PNG magic+boyut doğrula; `exec-out` yoksa `shell` +
  CRLF düzeltme fallback; stderr'i geri getir. (Başarısız entegrasyon testini
  de onarır.)
- **A3** APK export split'leri sessizce düşürüyor → tüm `pm path` satırları +
  split sayısını göster. **I2** `install-multiple`. **I1** install/uninstall
  `Success`/`Failure` satırını parse et (exit 0'da bile). **A2** path'siz
  `package:` satırlarını düşürme.
- **FR-1** frida arch: boş ABI'de `abilist`/`uname -m` fallback. **FR-2** truncated
  `comm`'u `cmdline` ile ayır. **FR-3** SELinux exec hatasını arch yerine doğru
  raporla. **FR-4** `grep`→glob, ISO-date fallback.
- **SC-2** scrcpy v2-only `--video-codec`'i koşullu yap (scrcpy v1 onarımı).
  **SC-1** Linux snap/flatpak + Windows portable lookup.
- **P5** stats: `MemAvailable` yoksa `MemFree+Buffers+Cached`; unset≠0.
  **P6** battery sysfs fallback. **P9** `dumpsys wifi | grep` → host-side parse.

## Faz 4 — Frontend kullanılabilirlik UX'i (kesişen)

- Paylaşılan `FeatureState` enum + `FeatureNotice` bileşeni (ui.tsx konvansiyonu).
- `App.tsx:61-70` global `unhandledrejection` susturucusunu daralt/kaldır.
- `.catch(()=>{})` ve catch'siz loader'lara hata durumu ekle (Frida list,
  GetNetworkInfo, ListConnections, Forwards, Processes "Starting…" takılması).
- Backend kontratı: connections/apps/stats/proc-logcat status için zengin
  availability struct'ları (IPTBackendInfo/TcpdumpInfo şablonu).

---

## Kesişen temizlik

53de802 ("drop awk/head/ps/top/pidof") şu dosyaları atladı — `head`/`grep`
kalıntıları: `iptables.go` (head -1), `tcpdump.go` (InstallTcpdump head -1),
`devices.go` (dumpsys wifi | grep), `frida.go` (ls | grep).

## Çalışma kuralı

Her faz: küçük commit'ler; her commit `gofmt`/`go vet ./...`/`go test ./...`
(ADBQ_SKIP_DEVICE=1) yeşil. Cihaz-bağımlı doğrulama gerçek cihaz/emülatörlerde.
