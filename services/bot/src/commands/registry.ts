import {
  ApplicationCommandOptionType,
  type ApplicationCommandDataResolvable,
} from "discord.js";
import { DateTime } from "luxon";
import { ZONE } from "../render/dates.js";

/**
 * The example dates in the descriptions come from the clock, not from the
 * source file. Written in, they age badly — a hardcoded 2026 still reads 2026
 * in 2028. They are fixed when the commands are registered, which happens once
 * per start.
 */
function examples(at: DateTime<true> = DateTime.now().setZone(ZONE) as DateTime<true>): { day: string; month: string } {
  // Before 18:35 the newest result is yesterday's, so the example is a date
  // that actually has one: copying it verbatim gives a result rather than
  // "chưa có kết quả".
  const settled = at.hour > 18 || (at.hour === 18 && at.minute >= 35) ? at : at.minus({ days: 1 });
  // The example month is the previous one. An example that matches the default
  // teaches nothing.
  const previous = at.startOf("month").minus({ days: 1 });
  return {
    day: settled.toFormat("dd/MM/yyyy"),
    month: previous.toFormat("MM/yyyy"),
  };
}

export function slashCommands(at?: DateTime<true>): ApplicationCommandDataResolvable[] {
  const { day, month } = examples(at);

  return [
    {
      name: "xsmb",
      description: "Kết quả xổ số miền Bắc",
      options: [
        {
          type: ApplicationCommandOptionType.String,
          name: "ngay",
          description: `Ngày cần xem, ví dụ ${day}. Bỏ trống thì lấy kỳ mới nhất.`,
        },
      ],
    },
    {
      name: "quaythu",
      description: "Quay thử một bảng XSMB cho vui — số ngẫu nhiên, không phải kết quả thật",
    },
    {
      name: "thongke",
      description: "Thống kê từ kho dữ liệu",
      options: [
        {
          type: ApplicationCommandOptionType.Subcommand,
          name: "ngay",
          description: "Phân tích một kỳ: kép, nháy, đầu đuôi câm, chạm",
          options: [
            {
              type: ApplicationCommandOptionType.String,
              name: "ngay",
              description: `Ngày cần xem, ví dụ ${day}. Bỏ trống thì lấy kỳ mới nhất.`,
            },
          ],
        },
        {
          type: ApplicationCommandOptionType.Subcommand,
          name: "db",
          description: "Bảng giải đặc biệt cả tháng",
          options: [
            {
              type: ApplicationCommandOptionType.String,
              name: "thang",
              description: `Tháng cần xem, ví dụ ${month}. Bỏ trống thì lấy tháng này.`,
            },
          ],
        },
        {
          type: ApplicationCommandOptionType.Subcommand,
          name: "logan",
          description: "Lô lâu chưa về nhất, kèm kỷ lục gan",
        },
        {
          type: ApplicationCommandOptionType.Subcommand,
          name: "degan",
          description: "Như logan nhưng chỉ tính giải đặc biệt",
        },
        {
          type: ApplicationCommandOptionType.Subcommand,
          name: "tanso",
          description: "Tần suất về, gom 100 số thành 10 ô",
          options: [
            {
              type: ApplicationCommandOptionType.String,
              name: "kieu",
              description: "Gom theo đầu, đuôi, tổng hay chạm",
              required: true,
              choices: [
                { name: "đầu", value: "dau" },
                { name: "đuôi", value: "duoi" },
                { name: "tổng", value: "tong" },
                { name: "chạm", value: "cham" },
              ],
            },
            {
              type: ApplicationCommandOptionType.Integer,
              name: "ngay",
              description: "Cửa sổ tính, mặc định 30 ngày",
              minValue: 1,
            },
          ],
        },
        {
          type: ApplicationCommandOptionType.Subcommand,
          name: "lo",
          description: "Hồ sơ đầy đủ của một số",
          options: [
            {
              type: ApplicationCommandOptionType.String,
              name: "so",
              description: "Số hai chữ số, ví dụ 27",
              required: true,
            },
          ],
        },
        {
          type: ApplicationCommandOptionType.Subcommand,
          name: "kho",
          description: "Kho dữ liệu đang có gì",
        },
      ],
    },
    {
      name: "gold",
      description: "Giá vàng hiện tại",
    },
    {
      name: "bieudo",
      description: "Biểu đồ lịch sử giá vàng",
      options: [
        {
          type: ApplicationCommandOptionType.String,
          name: "ma",
          description: "Mã vàng, ví dụ SJC. Bỏ trống thì lấy SJC.",
        },
        {
          type: ApplicationCommandOptionType.Integer,
          name: "ngay",
          description: "Số ngày lịch sử, mặc định 30",
          minValue: 2,
          maxValue: 3650,
        },
      ],
    },
    {
      name: "huongdan",
      description: "Danh sách lệnh",
    },
    {
      name: "thongbao",
      description: "Bật hoặc tắt thông báo kết quả hằng ngày cho kênh này",
      options: [
        {
          type: ApplicationCommandOptionType.String,
          name: "trangthai",
          description: "Bật hay tắt",
          required: true,
          choices: [
            { name: "bật", value: "bật" },
            { name: "tắt", value: "tắt" },
          ],
        },
      ],
    },
  ];
}
