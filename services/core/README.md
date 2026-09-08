# core

Sở hữu kho dữ liệu. Crawl xoso.com.vn và vang.today, lưu PostgreSQL, tính thống
kê, và phục vụ tất cả qua contract trong `../../contracts/openapi.yaml`.

Nó không biết Discord là gì. Client chat chỉ là một bên tiêu thụ API này, thay
được mà không phải đụng gì ở đây — và thêm bên thứ hai cũng vậy.

```
core                      chạy service
core fetch [ngày]         crawl một ngày rồi in ra
core backfill [từ ngày]   lấp kho rồi thoát
core gold                 in bảng giá vàng rồi thoát
```

## Cấu hình

| | |
|---|---|
| `DATABASE_URL` | bắt buộc |
| `API_ADDR` | mặc định `:8080`, chỉ trong compose network |
| `GOLD_URL` | **rỗng = tắt hẳn tính năng giá vàng** |
| `GOLD_TTL` / `GOLD_GRACE` | cache bảng giá vàng |
| `XOSO_BASE_URL` | đổi trang nguồn, dùng cho test |
| `BACKFILL_ON_START` | kéo kho ở nền lúc khởi động, mặc định bật |
| `BACKFILL_DAYS` | 0 = cả kho từ 01/10/2005 |
| `BACKFILL_CONCURRENCY` | số worker |

Không có biến nào của Discord. Nếu một cái quay lại, nghĩa là chat đã rò rỉ vào
service sở hữu database — `internal/config` có test riêng canh chuyện đó.

## Kho dữ liệu không phụ thuộc vào thông báo

Một vòng đối chiếu chạy độc lập: mỗi nhịp hỏi ngày mới nhất đã công bố là ngày
nào, và kho đã có chưa. Nhanh 20 giây trong khoảng 18h25–19h20, thong thả 15
phút phần còn lại.

Không có mốc nào để lỡ. Chết lúc 18h30 lên lại lúc 19h10 thì nhịp đầu tiên thấy
kho thiếu và đi lấy. Không cần queue, không cần scheduler: việc cần làm suy ra
được từ dữ liệu, nên chẳng có gì phải ghi nhớ giữa hai lần chạy.

Trước đây việc crawl là **tác dụng phụ** của việc đăng thông báo, nên tắt kênh
đăng ký cuối cùng là kho ngừng cập nhật luôn — không ai đoán được tắt thông báo
lại có hậu quả đó.

## Vài chỗ tinh tế

**Absence là cửa một chiều.** Đánh dấu một ngày "không quay" rồi thì `Get`
không crawl lại nữa, `Backfill` cũng đi qua `Get` nên cũng chịu. Chỉ `SaveDraw`
xoá được absence, mà không đường nào dẫn tới đó nữa. Vì thế phải qua 18h35 cộng
45 phút mới dám ghi: nguồn công bố dần từng giải, nên trang trống ngay sau mốc
hầu như luôn là "chưa tới lượt".

**Khoá cache thống kê là cặp (ngày mới nhất, bộ đếm ghi).** Chỉ dùng ngày mới
nhất là chưa đủ — backfill lấp các ngày **cũ hơn** ngày mới nhất, `max(draw_date)`
không nhúc nhích, và cache không rớt. Đó là một lỗi thật, `thongke db` từng kẹt
ở 3 kỳ cho tới khi có kỳ mới.

**Ghi trùng ngày bị gộp.** Nhiều request cùng lúc cho một ngày chỉ tạo một lần
crawl. Request cách xa nhau thì không — chúng bị chặn bằng một ghi chú "vừa
hỏi, nguồn chưa có" sống 10 giây.

**Thông báo là lease, không phải dấu vĩnh viễn.** Bảng `announcements` có
`sent_at`; một claim chưa gửi quá 10 phút thì lần sau lấy lại được. Nếu không,
một cú crash đúng giữa lúc claim và gửi là mất luôn ngày đó.

Exactly-once không có ở đây: Discord không có idempotency key. Chỉ chọn được
sai theo hướng nào, và thiết kế này nghiêng về **im lặng**.

**Gan đếm theo ngày lịch**, tần suất đếm nháy (hai giải cùng đuôi tính hai
lần). Cột kỷ lục là khoảng gan dài nhất số đó từng có — nó mới làm con số hiện
tại đọc được.

**Giá vàng** cache vài phút, không lưu database. Nguồn hỏng mà cache còn trong
hạn thì trả bảng cũ kèm mốc thời gian của chính nguồn; quá 6 tiếng thì báo lỗi,
vì một biểu đồ cũ một ngày mà không nói gì còn tệ hơn.

Mã vàng là của riêng nguồn: `SJL1L10` (vàng miếng SJC), `VNGSJC`, `DOHNL`,
`PQHNVM`, `XAUUSD`… — không phải `SJC` hay `PNJ` như phỏng đoán tự nhiên. Xin
một mã không tồn tại thì lỗi nói rõ mã nào thật sự có.

**Quay thử** sinh 27 số ngẫu nhiên nhưng không đi qua đường ghi nào, nên không
thể lọt vào kho. Đó là bảo đảm bằng cấu trúc chứ không phải bằng trí nhớ.

## Kiểm tra nguồn

Không cần token, không cần database:

```bash
make fetch DATE=03/09/2026
make gold
```

Parser bám vào tên class trên trang nguồn; trang đổi markup thì `make fetch`
báo `incomplete draw` ngay, sửa một mảng trong `internal/provider/xoso.go`.

## Phát triển

Máy không có Go vẫn chạy được, qua Docker:

```bash
docker run --rm -v ${PWD}:/src -w /src golang:1.22-alpine \
  sh -c "gofmt -l cmd internal && go build -mod=vendor ./... && go test -mod=vendor ./..."
```

`-race` cần cgo, mà Alpine không có. Dùng `golang:1.22` (bản Debian) cho lần
đó. Repo có nhiều goroutine nên đáng chạy một lần trước khi đụng vào chúng.

Test cần PostgreSQL thật tự bỏ qua nếu không có `TEST_DATABASE_URL` — nghĩa là
chúng mục dần mà không ai biết. Lease thông báo sống hoàn toàn trong SQL và đã
không được chạy thử suốt vì đúng lý do đó.

```bash
make pgtest
```

Nó chạy trên database `xsmb_test`, **không phải** database thật: harness gọi
`TRUNCATE` mọi bảng nó đụng tới, trỏ nhầm là mất cả kho.

### fakecore

`cmd/fakecore` là core chạy trên kho in-memory, để test TypeScript chạm vào
handler Go thật thay vì một mock. Sau khi bot Go biến mất, nó là cầu nối duy
nhất giữa hai ngôn ngữ trong test — đừng xoá.

```bash
go run ./cmd/fakecore :8099        # vàng tắt
go run ./cmd/fakecore :8098 gold   # vàng bật
```

Vàng tắt mặc định là cố ý: để lần chạy thường vẫn phủ nhánh `not_configured` mà
client nào cũng phải xử lý. Và nó chỉ nhận mã vàng có thật — một fake dễ dãi
hơn thứ nó đóng thế sẽ che đúng lớp lỗi đã từng lọt: bot mặc định mã `SJC`,
không tồn tại, mà mọi test vẫn xanh.

### Cây thư mục

```
cmd/core/            main, kèm lệnh fetch / gold / backfill
cmd/fakecore/        core trên kho in-memory, cho test TypeScript
internal/
  domain/            luật xổ số, ngày tháng, giá vàng, quay thử
  htmlscan/          tokenizer HTML
  provider/          crawler xoso.com.vn, client vang.today
  storage/           PostgreSQL, migration, bản in-memory
  service/           cache, gộp request trùng, ingest, backfill
  present/           vẽ bảng monospace
  chart/             vẽ PNG
  format/            định dạng số
  httpapi/           HTTP handler
```

Phụ thuộc một chiều: `httpapi` → `service` → `provider` + `storage` → `domain`.

`present` vẽ bảng cho `httpapi`, vì cách bày một bảng XSMB là kiến thức domain
chứ không phải khái niệm của Discord: giải nào mấy số, mấy chữ số, bao nhiêu ô
lọt một dòng điện thoại. Viết lại nó ở phía client là viết lại một lần nữa thứ
có thể lệch đi.

Dependency đã vendor sẵn, build offline được. Một thư viện ngoài duy nhất:

```
github.com/lib/pq
```

Không router, không ORM, không thư viện parse HTML, không thư viện migration,
không thư viện vẽ biểu đồ. Bốn cái cuối nằm trong `internal/htmlscan`,
`internal/storage` và `internal/chart`.
