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
Ölçümler her zaman yazdırılır. Bütçe ihlalinin **testi kırması** için:
```sh
ADBQ_SPAWN_BUDGET=1 go test ./internal/adb -run TestSpawnBudget -v
```

### Profil (yalnız dev build)
```sh
go build -tags pprof -o adbq-pprof . && ./adbq-pprof
go tool pprof -http=: 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30'
```
`pprof` build etiketinin arkasındadır; release ikilisinde handler'lar hiç derlenmez.

---

## 2. Baseline — 2026-08-18, hiçbir toplu-okuma çalışması yapılmadan önce

Cihaz: **a physical device over USB**.

| Ölçüm | Değer |
|---|---|
| Isınmış `ListDevices` + `Enrich` (bir poll) | **10,0 süreç** |
| `GetStats` (bir Overview yenilemesi) | **9 süreç** |
| `ListConnections` (bir Network yenilemesi) | **4 süreç** |
| **Boşta steady state** (5 sn cihaz + 2,5 sn stat poll'u) | **4,13 süreç/sn** |

Isınmış poll'un komut dağılımı (3 döngü):

```
shell getprop   12      shell ip        3
shell id         3      shell cat       3
shell ls         3      devices         3
shell uname      3
```

Boşta 10 saniyede `shell cat` 21 kez — `GetStats`'ın `/proc` okumaları baskın.

> Gerçek uygulamada bu sayı biraz daha yüksektir: yukarıdaki senaryo yalnızca Overview'ın
> iki poll'unu modelliyor. Uygulamada ayrıca iki `ScrcpyActive` poller'ı ve her ekran
> değişiminde önbelleksiz çalışan bir rozet-sayaç efekti var.

**Hedef:** boşta **< 1,0 süreç/sn**, pencere arka plandayken **~0**.

---

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

## 4. Bütçeler

`internal/adb/spawnbudget_device_test.go` içinde kodlanmıştır.

| Yol | Bütçe | Gerekçe |
|---|---|---|
| Isınmış `ListDevices`+`Enrich` | ≤ 4 süreç | Cihaz listesi + sınırlı sayıda gerçekten canlı okuma |
| `GetStats` | ≤ 3 süreç | Hepsi `/proc` ve `dumpsys`; tek tura sığar |
| `ListConnections` | ≤ 1 süreç | `connectionsRemote()` zaten tek komut olarak render ediyor |
| Boşta steady state | ≤ 1,0 süreç/sn | — |

Bütçeler hedef değil, **üst sınırdır**: amaçları, ileride bir değişikliğin okuma başına
tur ekleyip bunu bir yıl boyunca sessizce kullanıcının CPU'sundan ödetmesini engellemek.
