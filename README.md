# KaTools - Kathana

KaTools adalah aplikasi bantu otomasi untuk **Kathana - The Reign of Shadow**.

Gunakan sesuai aturan server/game yang berlaku dan dengan risiko sendiri.

## Sebelum mulai

- Gunakan Windows 10 atau Windows 11.
- Pastikan bahasa game menggunakan **English**. Fitur pembacaan dialog dan nama target tidak dirancang untuk bahasa Indonesia.
- Jalankan game dan KaTools dengan level izin yang sama. Jika game dijalankan sebagai Administrator, KaTools juga harus Administrator.
- Jangan memindahkan atau menghapus folder `important_file` dari paket KaTools.

## Cara pakai

1. Jalankan `KaTools.exe`.
2. Buka `http://127.0.0.1:8787` jika halaman KaTools belum terbuka otomatis.
3. Pilih jendela game pada bagian **Target Window**.
4. Tekan **SET AREA** untuk fitur yang ingin dipakai.
5. Atur hotkey, interval, dan fitur yang dibutuhkan.
6. Tekan **START**.

Saat memilih area, jangan sampai panel game yang dipilih tertutup jendela lain.

## Fitur

- Auto Accept Party
- Auto Potion HP dan TP
- Auto Pause on Death dan Auto Resu
- Emergency Skill saat HP rendah
- Assist Skill
- Target otomatis
- Target Until Dead
- Attack, Pick, serta skill `1`-`0` dan `F1`-`F10`

## SET AREA

| Area | Kegunaan |
| --- | --- |
| Target Panel Area | Target Until Dead dan Emergency Skill yang membutuhkan target |
| Party Area | Auto Accept Party |
| Death Dialog | Auto Pause on Death dan Auto Resu |
| Status HP / TP | Auto Potion dan Emergency Skill |

Untuk Status HP/TP, pilih dua bar HP dan TP beserta angkanya. Untuk Target Panel, pilih nama target dan bar HP merah target dalam satu area.

## Skip Target Names dan Target Until Dead

Isi **Skip Target Names** dengan nama yang tidak boleh diserang, dipisahkan memakai titik koma. Fitur ini dapat dipakai pada mode **Target** maupun **Target Until Dead**.

```text
NamaKarakterSaya;NamaPartyLeader
```

Pilih role **Attacker** untuk karakter penyerang atau **Support** untuk karakter support.

Pada mode Support, skill `1`-`0` memiliki pilihan **With Target**:

- Dicentang: skill boleh digunakan saat target monster masih ada.
- Tidak dicentang: skill menunggu target monster hilang.

## Pengaturan saat bot berjalan

Perubahan hotkey, interval, mode, daftar skip, dan pilihan fitur langsung diterapkan saat bot berjalan. Tidak perlu Stop/Start untuk perubahan biasa.

Stop bot terlebih dahulu bila ingin mengganti jendela game.

Tombol **SAVE CONFIG** menyimpan preset pengaturan untuk dipakai lagi nanti.

## Jika ada masalah

- Pastikan game memakai bahasa **English**.
- Pastikan area yang dipilih sudah benar dan tidak tertutup UI lain.
- Pastikan game dan KaTools memakai level izin yang sama.
- Coba Stop lalu Start bot jika baru mengubah tampilan UI game secara besar-besaran.
- Jika KaTools tidak bisa membaca apa pun, tutup KaTools lalu jalankan kembali.
