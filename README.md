# xsmb-discord-bot

Bot Discord tra kết quả xổ số miền Bắc và giá vàng.

```
/xsmb                 kết quả mới nhất
/thongke logan        lô lâu chưa về nhất
/quaythu              quay thử một bảng cho vui
/bieudo               biểu đồ giá vàng
```

Dạng `!xsmb` cũ vẫn chạy song song. 18h35 hằng ngày bot tự đăng kết quả vào các
kênh đã bật thông báo.

## Kiến trúc

Hai service, một database.

```
     người dùng Discord
             │
             ▼
     ┌───────────────┐         ┌──────────────────────────┐
     │ bot           │  HTTP   │ core                     │
     │ TypeScript    │────────▶│ Go                       │
     │ chỉ hiển thị  │         │ crawl, thống kê, cache   │
     └───────────────┘         └────────────┬─────────────┘
                                            │
                                    ┌───────┴────────┐
                                    ▼                ▼
                              PostgreSQL      xoso.com.vn
                                              vang.today
```

`bot` không có driver database và không biết trang nguồn ở đâu. Mọi con số nó
hiển thị đều đi qua contract trong `contracts/openapi.yaml`. Kiểm được:

```bash
cd services/core && go list -deps ./internal/httpapi | grep -c lib/pq   # có
ls services/bot/src/**/*.go 2>/dev/null | wc -l                         # 0
```

Contract để ở `contracts/` chứ không nằm trong service nào, vì không bên nào sở
hữu nó. Phía TypeScript **sinh** type từ nó — trong Go, đổi tên field là lỗi
biên dịch; qua ranh giới ngôn ngữ thì chỉ codegen mới lấy lại được điều đó.

## Chạy

```bash
cp .env.example .env      # điền DISCORD_TOKEN và POSTGRES_PASSWORD
docker compose up -d --build
docker compose logs -f bot
docker compose logs -f core
```

Migration tự chạy khi **core** khởi động. Dữ liệu nằm trong volume `pgdata`.

`bot` im lặng là bình thường — nó chỉ ghi log khi lỗi. Ba dòng lúc khởi động là
toàn bộ những gì nó nói trong một ngày yên ả. `core` thì nói nhiều hơn.

## Đọc tiếp

| | |
|---|---|
| [`services/core/README.md`](services/core/README.md) | Kho dữ liệu, crawler, thống kê, HTTP API. Cấu hình phía core. |
| [`services/bot/README.md`](services/bot/README.md) | Danh sách lệnh, cài đặt Discord, cấu hình phía bot. |
| [`contracts/README.md`](contracts/README.md) | Thoả thuận giữa hai service, và bên nào sinh code. |

## Bảo mật

Core API **không có xác thực**, cố ý. Nó không phơi cổng nào ra host, và Docker
cô lập các bridge network, nên chỉ `db` và `bot` chạm được. Kẻ tấn công phải
chiếm được một trong hai — mà chiếm được thì token dùng chung nằm cùng chỗ với
mọi bí mật khác, chẳng cản gì.

Publish cổng 8080 là phá đúng giả định đó. Nếu cần, thêm xác thực **cùng lúc**,
đừng để sau. `docker-compose.yml` ghi lại điều này ngay tại dòng sẽ sửa.

## Cây thư mục

```
contracts/openapi.yaml     thoả thuận giữa hai service
services/
  core/                    Go — crawl, thống kê, PostgreSQL, HTTP API
  bot/                     TypeScript — Discord
docker-compose.yml
```

`POSTGRES_PASSWORD` bắt buộc, thiếu thì compose từ chối chạy. Các biến còn lại
chia theo service, xem README tương ứng.
