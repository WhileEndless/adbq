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

## 2. Ölçümler — a physical device over USB

| Ölçüm | Önce | Sonra | Kazanç |
|---|---|---|---|
| Isınmış `ListDevices` + `Enrich` (bir poll) | 10,0 süreç | **2,0** | 5,0× |
| `GetStats` (bir Overview yenilemesi) | 9 süreç | **1** | 9× |
| `ListConnections` (bir Network yenilemesi) | 4 süreç | **1** | 4× |
| **Boşta steady state** | **4,13 süreç/sn** | **0,57** | **7,2×** |

| Cihaz takıldığında (soğuk cache) | ~30 süreç | **10** | 3× |

Duvar saati de düştü: ısınmış poll 1,64 sn → 0,65 sn (3 döngü).

Soğuk bağlanma özellikle önemli: adb eşzamanlılık altında çok kötü davranıyor —
bu cihazda **40 eşzamanlı `adb shell` 3 dakikadan uzun sürdü**, seri hâlde ~55 ms/çağrı
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

**Kalan hedef:** pencere arka plandayken **~0** (Faz 3: görünürlük kapısı + track-devices).

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
| Isınmış `ListDevices`+`Enrich` | ≤ 4 süreç | Cihaz listesi + tek dinamik probe (bugün 2) |
| `GetStats` | ≤ 3 süreç | Hepsi `/proc` ve `dumpsys`; tek tura sığar (bugün 1) |
| `ListConnections` | ≤ 1 süreç | Dört tablo tek turda, sentinel'la ayrılıyor |
| Boşta steady state | ≤ 1,0 süreç/sn | bugün 0,57 |

Bütçeler hedef değil, **üst sınırdır**: amaçları, ileride bir değişikliğin okuma başına
tur ekleyip bunu bir yıl boyunca sessizce kullanıcının CPU'sundan ödetmesini engellemek.
