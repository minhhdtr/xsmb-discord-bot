# xsmb-discord-bot

Bot Discord tra kết quả xổ số miền Bắc và giá vàng.

```
/xsmb                 kết quả mới nhất
/xsmb ngay:14/08/2026 kết quả một ngày
/thongke logan        lô lâu chưa về nhất
/gold                 giá vàng
/bieudo               biểu đồ 30 ngày
```

Dạng `!xsmb` cũ vẫn chạy song song.

18h35 hằng ngày bot tự đăng kết quả vào các kênh đã bật thông báo.

Go + PostgreSQL, chạy bằng Docker. Dữ liệu lấy trực tiếp từ xoso.com.vn và
vang.today.

## Chạy

```bash
cp .env.example .env      # điền DISCORD_TOKEN
docker compose up -d --build
docker compose logs -f bot
```

Migration tự chạy khi bot khởi động. Dữ liệu nằm trong volume `pgdata`.

### Message Content Intent

Chỉ cần nếu bạn dùng dạng `!xsmb`. Slash command không cần.

Vào [Developer Portal](https://discord.com/developers/applications) → app của bạn →
tab **Bot** → bật **MESSAGE CONTENT INTENT** → Save. Không bật mà vẫn để
`PREFIX_COMMANDS=true` thì Discord từ chối luôn kết nối gateway.

Muốn chỉ dùng slash thì đặt `PREFIX_COMMANDS=false`, khỏi cần intent nào đặc quyền.

Mời bot vào server, thay `APPLICATION_ID` bằng ID của app:

```
https://discord.com/oauth2/authorize?client_id=APPLICATION_ID&scope=bot&permissions=84992
```

84992 = View Channel + Send Messages + Embed Links + Read Message History.

## Lệnh

| Slash | Prefix | |
|---|---|---|
| `/xsmb` | `!xsmb` | Kết quả mới nhất. Trước 18h35 trả kết quả hôm qua. |
| `/xsmb ngay:14/08/2026` | `!xsmb 14/08/2026` | Một ngày. Nhận cả `14-08-2026`, `2026-08-14`. |
| `/thongbao trangthai:bật` | `!xsmb sub` | Bật/tắt thông báo 18h35. Cần quyền Quản lý kênh. |
| `/thongke kho` | `!xsmb status` | Kho dữ liệu đang có gì. |
| `/thongke logan` | `!xsmb logan` | Lô lâu chưa về nhất, kèm kỷ lục gan. |
| `/thongke degan` | `!xsmb degan` | Như trên, chỉ tính giải đặc biệt. |
| `/thongke tanso` | `!xsmb tanso` | Tần suất, mặc định 30 ngày. |
| `/thongke lo so:88` | `!xsmb lo 88` | Hồ sơ một số. |
| `/thongke db thang:08/2026` | `!xsmb db 08/2026` | Bảng giải đặc biệt cả tháng. |
| `/huongdan` | `!xsmb help` | |
| `/gold` | `!gold` | Giá vàng trong nước và thế giới. |
| `/bieudo` | `!gold chart` | Biểu đồ, mặc định vàng miếng SJC 30 ngày. |

Slash command đăng ký lại toàn bộ mỗi lần bot khởi động, nên đổi tên lệnh không để
sót lệnh cũ. Đặt `DISCORD_GUILD_ID` khi dev để lệnh hiện ra ngay thay vì chờ tới một
tiếng.

Discord huỷ interaction nếu không phản hồi trong 3 giây, mà crawl một ngày chưa có
hoặc tính thống kê lần đầu thì lâu hơn thế. Nên mọi lệnh đều báo nhận trước rồi sửa
câu trả lời vào sau.

<details>
<summary>Bảng cũ chỉ có prefix</summary>

| | |
|---|---|
| `!xsmb` | Kết quả mới nhất. Trước 18h35 trả kết quả hôm qua. |
| `!xsmb 14/08/2026` | Một ngày. Nhận cả `14-08-2026`, `2026-08-14`, `14/08`. |
| `!xsmb sub` / `unsub` | Bật/tắt thông báo 18h35 cho kênh này. Cần quyền Quản lý kênh. |
| `!xsmb status` | Kho dữ liệu đang có gì. |
| `!xsmb logan` | 12 số lô lâu chưa về nhất, kèm kỷ lục gan. |
| `!xsmb degan` | Như trên, chỉ tính giải đặc biệt. |
| `!xsmb tanso [ngày]` | Tần suất, mặc định 30 ngày. |
| `!xsmb lo 88` | Hồ sơ một số. |
| `!xsmb db 08/2026` | Bảng giải đặc biệt cả tháng. |
| `!xsmb help` | |
| `!gold` | Giá vàng trong nước và thế giới. |
| `!gold chart [mã] [ngày]` | Biểu đồ, mặc định vàng miếng SJC 30 ngày. |
| `!gold help` | Kèm danh sách mã vàng. |

</details>

## Cấu hình

Xem `.env.example`. Những cái hay dùng:

| | |
|---|---|
| `DISCORD_TOKEN` | bắt buộc |
| `COMMAND_PREFIX` / `GOLD_PREFIX` | mặc định `!xsmb` / `!gold` |
| `DISCORD_GUILD_ID` | đăng ký slash command vào một server, hiện ra ngay |
| `PREFIX_COMMANDS` | mặc định bật; tắt thì khỏi cần Message Content intent |
| `BACKFILL_ON_START` | kéo kho dữ liệu ở nền lúc khởi động, mặc định bật |
| `BACKFILL_DAYS` | 0 = cả kho từ 01/10/2005 |
| `GOLD_TTL` / `GOLD_GRACE` | cache giá vàng |

## Ghi chú

**Backfill** chạy nền ngay lần khởi động đầu, quét từ ngày mới về ngày cũ, bỏ qua
ngày đã có nên restart bao nhiêu lần cũng vô hại. Nếu nguồn bắt đầu từ chối thì
dừng luôn thay vì cày tiếp.

**Thông báo 18h35** không bắn một phát rồi thôi. Trang nguồn công bố các giải dần
dần, nên bot hỏi lại mỗi 20 giây cho tới khi đủ 27 số. Kiểu `Prizes` chỉ dựng được
khi đủ 27 số, nên trang thiếu giải hỏng ngay ở tầng parse chứ không lọt xuống
Discord.

Không gửi trùng: mỗi cặp (ngày, kênh) là khoá chính trong bảng `announcements`.
Restart hay chạy hai instance đều không đăng lại.

**Trước 18h35 `!xsmb` trả kết quả hôm qua** chứ không báo lỗi.

**Thống kê** chạy thẳng trên bảng `draws`, cache đến khi có kỳ mới. Gan đếm theo
ngày lịch, tần suất đếm nháy (hai giải cùng đuôi tính hai lần). Cột kỷ lục là
khoảng gan dài nhất số đó từng có — nó mới làm con số hiện tại đọc được.

Bot không có lệnh dự đoán. Footer mọi bảng ghi rõ mỗi kỳ quay độc lập.

**Giá vàng** cache vài phút, không lưu database. Nguồn hỏng mà cache còn mới thì
trả bảng cũ kèm mốc thời gian của chính nguồn.

## Kiểm tra nguồn

Không cần token, không cần database:

```bash
make fetch DATE=14/08/2026
make gold
```

Chạy trước khi cắm token. Parser bám vào tên class trên trang nguồn; trang đổi
markup thì `make fetch` báo `incomplete draw` ngay, sửa một mảng trong
`internal/provider/xoso.go`.

## Phát triển

```bash
make test
make race
make build
```

Test PostgreSQL tự bỏ qua nếu không có `TEST_DATABASE_URL`.

Dependency đã vendor sẵn, build được offline. Hai thư viện ngoài:

```
github.com/bwmarrin/discordgo
github.com/lib/pq
```

Không router, không ORM, không thư viện parse HTML, không thư viện migration,
không thư viện vẽ biểu đồ. Ba cái cuối nằm trong `internal/htmlscan`,
`internal/storage` và `internal/chart`.

```
cmd/xsmb-discord-bot/   main, kèm lệnh fetch / gold / backfill
internal/
  domain/       luật xổ số, ngày tháng, giá vàng
  htmlscan/     tokenizer HTML
  provider/     crawler xoso.com.vn, client vang.today
  storage/      PostgreSQL, migration, bản in-memory cho test
  service/      cache, singleflight, backfill
  chart/        vẽ PNG
  format/       định dạng số
  bot/          discordgo, router lệnh, hẹn giờ 18h35
```

Phụ thuộc một chiều: `bot` → `service` → `provider` + `storage` → `domain`.
