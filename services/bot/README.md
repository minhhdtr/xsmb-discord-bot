# bot

Client Discord. Không có database, không có crawler: mọi con số nó hiển thị đều
đến từ `core` qua contract trong `../../contracts/openapi.yaml`.

## Lệnh

| Slash | Prefix | |
|---|---|---|
| `/xsmb` | `!xsmb` | Kết quả mới nhất. Từ 18h25 đã thử lấy kỳ hôm nay, chưa có thì trả hôm qua. |
| `/xsmb ngay:03/09/2026` | `!xsmb 03/09/2026` | Một ngày. Nhận cả `3-9-2026`, `2026-09-03`, `03/09`, `3`, `hôm qua`. |
| `/quaythu` | `!xsmb quaythu` | Quay thử một bảng. Số ngẫu nhiên, không lưu vào kho. |
| `/thongbao trangthai:bật` | `!xsmb sub` / `unsub` | Bật/tắt thông báo hằng ngày. **Cần quyền Quản lý kênh.** |
| `/thongke ngay` | `!xsmb ngay` | Phân tích một kỳ: kép, nháy, đầu đuôi câm, chạm. |
| `/thongke db thang:08/2026` | `!xsmb db 08/2026` | Bảng giải đặc biệt cả tháng. |
| `/thongke logan` | `!xsmb logan` | Lô lâu chưa về nhất, kèm kỷ lục gan. |
| `/thongke degan` | `!xsmb degan` | Như trên, chỉ tính giải đặc biệt. |
| `/thongke tanso kieu:chạm` | `!xsmb tanso cham 90` | Gom 100 số thành 10 ô: đầu, đuôi, tổng, chạm. |
| `/thongke lo so:88` | `!xsmb lo 88` | Hồ sơ một số. |
| `/thongke kho` | `!xsmb status` | Kho dữ liệu đang có gì. |
| `/huongdan` | `!xsmb help` | |
| `/gold` | `!gold` | Giá vàng trong nước và thế giới. |
| `/bieudo` | `!gold chart` | Biểu đồ vàng miếng SJC 30 ngày. Ô `ma` có gợi ý mã. |

Dạng gõ tay nhận cả `!xsmb logan` lẫn `!xsmb thongke logan`.

Mã vàng là của riêng nguồn — `SJL1L10`, `VNGSJC`, `DOHNL`… — nên ô `ma` có gợi
ý, lấy từ chính bảng giá core đang cache. Gõ tay `SJC` là trượt: mã đó không
tồn tại, dù ai cũng đoán vậy.

## Cấu hình

| | |
|---|---|
| `DISCORD_TOKEN` | bắt buộc |
| `CORE_URL` | mặc định `http://core:8080` |
| `COMMAND_PREFIX` / `GOLD_PREFIX` | mặc định `!xsmb` / `!gold` |
| `DISCORD_GUILD_ID` | đăng ký slash vào một server, hiện ra ngay thay vì chờ tới một tiếng |
| `PREFIX_COMMANDS` | mặc định bật; tắt thì khỏi cần Message Content intent |

## Cài đặt Discord

### Message Content Intent

Chỉ cần nếu dùng dạng `!xsmb`. Slash command không cần.

Developer Portal → app → tab **Bot** → bật **MESSAGE CONTENT INTENT** → Save.
Không bật mà vẫn để `PREFIX_COMMANDS=true` thì Discord từ chối luôn kết nối
gateway.

### Mời bot

```
https://discord.com/oauth2/authorize?client_id=APPLICATION_ID&scope=bot&permissions=84992
```

84992 = View Channel + Send Messages + Embed Links + Read Message History.

## Vài chỗ tinh tế

**Slash command đăng ký lại toàn bộ mỗi lần khởi động**, nên đổi tên lệnh không
để sót lệnh cũ. Không đặt `DISCORD_GUILD_ID` thì lệnh là global và Discord mất
tới một tiếng để phát tán.

**Mọi lệnh đều báo nhận trước rồi sửa câu trả lời vào sau.** Discord huỷ
interaction nếu không phản hồi trong 3 giây, mà crawl một ngày chưa có thì lâu
hơn thế.

**Option đọc theo từng subcommand, không gộp một vòng lặp.** `ngay` là String
dưới `/thongke ngay` nhưng là Integer dưới `/thongke tanso`; đọc cả hai bằng
`getString` thì discord.js ném lỗi **trước** `deferReply`, và người dùng chỉ
thấy "The application did not respond".

**Quyền `/thongbao` kiểm hai lần**: `defaultMemberPermissions` để Discord ẩn
lệnh, cộng một kiểm tra lúc chạy — mặc định đó có thể bị guild ghi đè, và dạng
gõ tay thì không thấy nó bao giờ.

**Mọi đường vào đều phải bắt lỗi.** Node biến một unhandled rejection thành
exit 1, nên một `channel.send()` hỏng là bot chết rồi khởi động lại — và bất kỳ
ai cũng lặp lại được bằng cách gõ lệnh trong kênh bot không có quyền gửi.
`isSendable()` không cứu: nó kiểm loại kênh, không kiểm quyền. Có một
`process.on("unhandledRejection")` làm lưới cuối, nhưng đó là lưới chứ không
phải cách xử lý — nó nổ ra thường xuyên nghĩa là có chỗ thiếu `catch`.

**Thông báo 18h35 hỏi core, không tự crawl.** Core lo lấp kho theo lịch của
riêng nó; việc ở đây chỉ là nhận ra. Hỏi thay vì được đẩy giữ cho phụ thuộc đi
một chiều: bot biết core ở đâu, core không biết gì về bot. Thêm client thứ hai
không phải đụng vào core.

Timeout hay core lỗi tạm thời thì thử lại trong cửa sổ 45 phút; chỉ dừng khi
lỗi vĩnh viễn. Trước đây bất kỳ lỗi nào không phải "chưa có" cũng kết thúc luôn
cả ngày.

## Chạy test

    npm install
    npm run gen:api      # sinh lại src/core/schema.d.ts từ contract
    npm run check        # tsc --noEmit
    npm run check:api    # schema sinh ra có khớp contract không
    npm test             # tsc, rồi node --test trên bản đã biên dịch

Tests run on Node's built-in runner, so there is no test framework in the
dependency tree. That is not minimalism for its own sake: vitest pulls in vite,
which pulls in rollup and esbuild, which ship **per-platform native binaries**.
A lockfile written on Linux then installs the wrong binary on Windows and npm
reports it as a corrupt module. Nothing here needs more than `assert`, and
avoiding that class of problem entirely is worth more than nicer matchers.

`test/core.test.ts` talks to a real core rather than a mock. A mock would agree
with whatever the test author believed the contract said, which is the one
thing not worth checking once two languages read the same document.

    cd ../core && go run ./cmd/fakecore :8099          # in another terminal
    cd ../core && go run ./cmd/fakecore :8098 gold     # and a third, for gold
    CORE_REQUIRED=1 GOLD_CORE_URL=http://127.0.0.1:8098 npm test

Gold is off in the default fakecore on purpose, so the ordinary run exercises
the `not_configured` path every client has to handle. The second instance with
`gold` covers the rendering.

The runner takes one file at a time (`--test-concurrency=1`). The files share a
core, and therefore share its database; one of them clears every subscription,
which would otherwise wipe rows another file is asserting on.

Without a core those tests are **skipped**, and the runner says so. They are never
quietly passed: a green suite that checked nothing is worse than no suite, and
the first version of this file got that wrong.

`CORE_REQUIRED=1` turns an unreachable core into a hard failure. CI sets it.

### Cây thư mục

```
src/
  index.ts           nối gateway, phát frame, autocomplete
  core/              client HTTP, schema sinh từ contract
  commands/          định tuyến, đăng ký slash, thông báo 18h35
  render/            embed, bảng, ngày tháng, số
test/
```

## Keeping the generated client honest

`npm run check:api` regenerates the schema and fails if the result differs from
what is committed. CI runs it.

Deliberately a check rather than a `prebuild` hook. A hook would quietly
rewrite a committed file during every build, leaving a dirty working tree
nobody asked for and no signal that the contract had moved. Failing loudly is
the point.

## Regenerating the client

`src/core/schema.d.ts` is generated. Do not edit it, and do not hand-write the
types beside it either. In Go a renamed field was a compile error; here the
generator is the only thing that puts that back, and a hand-written interface
drifts in silence.

## About package-lock.json

Commit it. But note that `npm ci` with a lockfile written on another platform
is where the native-binary problem above comes from. Since the dependency tree
now has no native binaries at all, that risk is gone — keep it that way, and
think twice before adding a dependency that ships a `.node` file.
