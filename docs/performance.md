# Performans — ölçüm, sınıflandırma, bütçeler

adbq bir `adb` sarmalayıcısıdır. Maliyeti ağırlıkla **süreç yaratmadır**: her mantıksal
cihaz okuması bir `adb` istemcisi fork/exec eder, o istemci adb sunucusuna bağlanır, tek
komut çalıştırır ve çıkar. Ayrıştırma ve render bunun yanında gürültüdür.

Bu yüzden buradaki tek gerçek ölçü: **saniyede kaç `adb` süreci başlatıyoruz.**

---

## 1. Nasıl ölçülür

### Uygulama içinden
Settings → **adb load**. Saniyedeki süreç sayısı, pencere içindeki toplam, canlı akış
sayısı ve **en yoğun komut şekilleri** (`shell getprop`, `shell cat`, …) listelenir.
"Reset window" ile önce/sonra karşılaştırması yapılır.

Komut şekli düşük kardinalitelidir: cihaz seri numarası düşürülür, `adb shell` için uzak
komutun yalnızca ilk kelimesi tutulur. Yani liste "hangi çağıran çok süreç doğuruyor"
sorusunu yanıtlar, paket adı/pid gürültüsüne boğulmaz.

### Testten (gerçek cihaz gerekir)
```sh
go test ./internal/adb -run TestSpawnBudget -v
```
Bütçeler zorunludur: aşılırsa test kırılır. Cihaz yoksa test atlanır.

### Profil (yalnız dev build)
```sh
go build -tags pprof -o adbq-pprof . && ./adbq-pprof
go tool pprof -http=: 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
```
`pprof` build etiketinin arkasındadır; release ikilisinde handler'lar hiç derlenmez.

---

## 2. Ölçümler — fiziksel cihaz, USB

| Ölçüm | Önce | Sonra | Kazanç |
|---|---|---|---|
| Isınmış `ListDevices` + `Enrich` (bir poll) | 10,0 süreç | **2,0** | 5,0× |
| `GetStats` (bir Overview yenilemesi) | 9 süreç | **1** | 9× |
| `ListConnections` (bir Network yenilemesi) | 4 süreç | **1** | 4× |
| Cihaz takıldığında (soğuk cache) | ~30 süreç | **10** | 3× |
| **Boşta steady state** (Faz 2 sonrası) | 4,13 süreç/sn | 0,57 | 7,2× |
| **Boşta steady state** (push tracking ile) | **4,13 süreç/sn** | **0,027** | **153×** |

Duvar saati de düştü: ısınmış poll 1,64 sn → 0,65 sn (3 döngü).

Soğuk bağlanma özellikle önemli: adb eşzamanlılık altında çok kötü davranıyor —
test cihazında **40 eşzamanlı `adb shell` 3 dakikadan uzun sürdü**, seri hâlde ~55 ms/çağrı
iken. Cihazın takıldığı an, transport'u doyurmak için en kötü an.

Kalan iki süreç `adb devices -l` (Faz 3'te `track-devices` ile kalkacak) ve
`Enrich`'in tek batch probe'u.

Isınmış poll'un komut dağılımı (3 döngü):

```
önce                        sonra
shell getprop   12          devices    3
shell id         3          shell id   3
shell ls         3
shell ip         3
shell cat        3
shell uname      3
devices          3
```

> Senaryo Overview'ın iki poll'unu modelliyor. Uygulamada ayrıca iki `ScrcpyActive`
> poller'ı var (Faz 3'te olaya çevrilecek). Her ekran değişiminde önbelleksiz
> `pm list packages` çağıran rozet-sayaç efekti kaldırıldı.

Boştaki 0,027, dakikada bir çalışan güvenlik ağı poll'undan geliyor: adb sunucusu
transport değişikliklerini bildiriyor ama başka bir araçla yapılan `adb root` gibi
değişiklikleri bildirmiyor. 20 saniyelik bir pencerede ölçülen değer **0,00**.

Pencere gizliyken kalan tüm poll'lar da duruyor (`usePoll` görünürlük kapısı).

---

### Cihaz listesi: poll değil push

`adb devices -l` beş saniyede bir çalıştırılmıyor artık. adb sunucusunun
`host:track-devices` protokolüne kalıcı bir soket açılıyor (`internal/adb/track.go`,
yalnız stdlib) ve sunucu her değişiklikte listeyi kendisi gönderiyor. Sonuç: boşta
sıfır süreç, ve cihaz takıldığında **anında** görünüyor.

Fallback zorunlu: tracker, kullanıcının uygulama içinden öldürebildiği bir sunucuya
açılmış uzun ömürlü bir soket. Bağlantı düşerse arka uç poll'a döner ve **aynı olayı**
yayınlar — arayüzde tek bir abonelik var, fallback mantığını yanlış yapabileceği bir
yer yok. Hangi yolun aktif olduğu Settings → adb load altında görünür.

## 3. Veri volatilite sınıfları

Optimizasyonun dayandığı model: her cihaz bilgisi *ne sıklıkla değişebileceğine* göre
sınıflanır, hepsine aynı muamele yapılmaz.

| Sınıf | Tanım | Politika |
|---|---|---|
| **S0** | Cihaz bağlı kaldığı sürece değişemez | Bir kez oku, bağlantı kopunca unut. Diske de yazılabilir. |
| **S1** | Değişebilir ama nadiren — ve değiştiren çoğunlukla adbq'nun kendisi | Uzun TTL **+ olay bazlı invalidation** |
| **S2** | Gerçekten anlık | Cache yok; poll, ama görünürlük kapılı |
| **S3** | Durumu adbq üretiyor | Poll etme — olay yayınla |

**S0 örnekleri:** SDK/release/ABI, `ro.serialno`, `ro.product.*`, `ro.build.id|tags`,
`ro.hardware`, çekirdek sürümü, `MemTotal`, depolama toplamı, çekirdek sayısı,
iptables/tcpdump ikili varlığı.

**S1 örnekleri:** root durumu, kurulu uygulama listesi, sertifikalar, hosts, forward'lar,
Wi-Fi SSID, IP, batarya, depolama boş alanı.

**S2 örnekleri:** CPU%, `MemAvailable`, süreç tablosu, soket listesi.

**S3 örnekleri:** scrcpy çalışıyor mu, kayıt sürüyor mu, capture aktif mi, cihaz
takıldı/çıkarıldı, görev durumu.

---

## 4. Invalidation matrisi

Uzun TTL'i güvenli kılan şey bu tablo. Kural ve zorlaması: CLAUDE.md §4.2.

| Eylem | Düşen domain'ler |
|---|---|
| Uygulama kur / kaldır | `apps`, `storage` |
| Uygulama verisini temizle | `apps`, `storage` |
| Uygulama başlat / durdur | `apps` |
| Dosya sil / push | `files`, `storage` |
| mkdir / mv / chmod / chown | `files` |
| tcpdump kur | `tcpdump`, `files`, `storage` |
| frida-server push / başlat / durdur | `frida` (+ push'ta `files`, `storage`) |
| Sertifika kur | `certs` |
| hosts yaz / uygula | `hosts` (+ uygulamada `net`) |
| DNS flush | `net` |
| iptables (her yazma) | `iptables` |
| Forward / reverse ekle-sil | `forwards` |
| Proxy ayarla | `proxy`, `net` |
| Capture başlat / durdur | `tcpdump` |
| `tcpip` moduna geç | `net` |
| Profil uygula | `Profile.Domains()` — etkin adımlardan türetilir |
| **Reboot / power off / adbd restart** | **hepsi** (`props` dahil) |
| Bağlan / bağlantıyı kes | **hepsi** |
| SDK / jadx / AVD işlemleri | `sdk` / `jadx` / `avd` (host kapsamlı) |

Buna ek olarak **öz-iyileşme**: `ShellSU` beklenmedik bir `permission denied`
aldığında root probe'unu unutur. Başarısızlığın kendisi, herhangi bir zamanlayıcıdan
daha iyi bir bayatlık sinyali.

## 5. Poll envanteri

Kalan her zamanlayıcı `frontend/src/lib/poll.ts`'teki `usePoll`'dan geçer ve
**pencere gizliyken durur**. Ayrıca her biri konusunun değişip değişemeyeceğine
göre kapılıdır.

| Yer | Aralık | Kapı |
|---|---|---|
| Cihaz listesi | — | **poll yok**; `track-devices` push |
| scrcpy durumu | — | **poll yok**; olay |
| Overview göstergeleri | 2,5 sn | cihaz var |
| Süreç tablosu | 1/2/5 sn (kullanıcı seçer) | ekran açık, duraklatılmamış |
| Soket listesi | 3 sn | duraklatılmamış |
| Capture durumu (Network) | 3 sn aktif / 15 sn değilse | — |
| Canlı capture durumu | 1,5 sn | **yalnız capture çalışırken** |
| Uygulama çalışıyor mu | 5 sn | **yalnız detay paneli açıkken** |
| AVD listesi | 3 sn boot ederken / 20 sn | — |
| AVD root sekmesi | 30 sn | — |
| Cihaz güvenlik ağı (arka uç) | 60 sn | push aktifken; düşerse 5 sn |

## 6. Bütçeler

`internal/adb/spawnbudget_device_test.go` içinde kodlanmıştır.

| Yol | Bütçe | Gerekçe |
|---|---|---|
| Isınmış `ListDevices`+`Enrich` | ≤ 4 süreç | Cihaz listesi + tek dinamik probe (bugün 2) |
| `GetStats` | ≤ 3 süreç | Hepsi `/proc` ve `dumpsys`; tek tura sığar (bugün 1) |
| `ListConnections` | ≤ 1 süreç | Dört tablo tek turda, sentinel'la ayrılıyor |
| Boşta steady state (tek okuma yolu) | ≤ 1,0 süreç/sn | bugün 0,57 |
| Boşta uygulama, push aktif | ≤ 0,1 süreç/sn | bugün 0,00 (`TestAppIdleCost`) |
| Boşta uygulama, fallback poll | ≤ 1,0 süreç/sn | eski steady state'ten kötü olmamalı |

Bütçeler hedef değil, **üst sınırdır**: amaçları, ileride bir değişikliğin okuma başına
tur ekleyip bunu bir yıl boyunca sessizce kullanıcının CPU'sundan ödetmesini engellemek.

## 7. Akış yolları

Süreç sayısı dışında kalan iki maliyet kalemi.

**Canlı capture.** Paket başına dört syscall (iki okuma, tee üzerinden iki yazma)
vardı; iki uç da artık tamponlu. Çözülmüş paketler dilim + map yerine sabit bir
ring'te (`internal/adb/packetring.go`): tahliye paket başına **424 µs → 87 ns**
(100k kapasitede ölçüldü) — eskisi verimi ~2.360 paket/sn'ye sabitliyordu.
Emit ve decode oturum kilidinin dışına çıktı.

> Tampon bir sözleşme getirir: dosyayı **dışarıdan** okuyan her şey önce
> `FlushMirror()` çağırmalı, yoksa en yeni paketleri göremez. `SaveLivePcap` ve
> `Stop` çağırıyor.

**Shell.** PTY okuması başına bir Wails olayı vardı (4 KB'lık okumalar, yani
`top` çalıştırınca saniyede yüzlerce olay). Yaklaşık bir kare penceresinde
birleştiriliyor. Scrollback tee'si de tamponlu; diskten okuyan her şey önce
`flushShellLogs()` çağırıyor.

**Logcat.** Ekranı bir kez ziyaret etmek, feed'i oturum boyunca tam hızda
bırakıyordu. Akış açık kalıyor (geçmiş korunsun diye) ama ekran kapalıyken
**olay yayını duruyor**.
