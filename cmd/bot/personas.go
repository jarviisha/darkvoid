package main

import (
	"fmt"
	"strings"
)

// persona describes one bot account and the writing voice Gemini should adopt.
// Usernames must satisfy the user-service rule: [a-zA-Z0-9_-]{3,30}.
type persona struct {
	username    string
	displayName string
	style       string // voice/tone instruction embedded in the Gemini prompt
}

// personas is the pool of bot accounts. BOT_ACCOUNTS selects the first N.
// All accounts share BOT_PASSWORD and register with <username>@bot.local.
var personas = []persona{
	{
		username:    "bot_sky",
		displayName: "Sky Vũ",
		style:       "giọng trẻ trung, hài hước, hay dùng emoji, thỉnh thoảng chêm tiếng Anh kiểu dân văn phòng",
	},
	{
		username:    "bot_mien",
		displayName: "Miên Đặng",
		style:       "giọng trầm lắng, tản văn, viết về cảm xúc và quan sát đời thường, ít emoji",
	},
	{
		username:    "bot_codek",
		displayName: "Khoa Coder",
		style:       "giọng dev backend, kể chuyện nghề lập trình, tự trào, thích chia sẻ bài học kỹ thuật",
	},
	{
		username:    "bot_hafood",
		displayName: "Hà Foodie",
		style:       "giọng food blogger, mô tả món ăn sống động, hay rủ rê mọi người đi ăn",
	},
	{
		username:    "bot_dulich",
		displayName: "Phong Xê Dịch",
		style:       "giọng travel blogger, kể trải nghiệm những nơi vừa đi qua, giàu hình ảnh",
	},
}

// topics gives each generation a random subject so posts don't converge.
var topics = []string{
	"một bug khó chịu vừa gặp khi code và bài học rút ra",
	"quán cà phê mới phát hiện ở góc phố quen",
	"một món ăn đường phố Việt Nam đáng nhớ",
	"chuyến đi cuối tuần đến một tỉnh miền núi phía Bắc",
	"suy nghĩ về work-life balance của dân văn phòng",
	"cuốn sách hoặc bộ phim vừa xem xong",
	"thời tiết hôm nay và tâm trạng đi kèm",
	"một thói quen nhỏ giúp ngày làm việc dễ thở hơn",
	"kỷ niệm thời sinh viên chợt nhớ lại",
	"trải nghiệm học một kỹ năng mới",
	"chuyện dở khóc dở cười khi đi làm",
	"một góc Sài Gòn hoặc Hà Nội lúc sáng sớm",
	"cảm nghĩ về mạng xã hội và thói quen lướt điện thoại",
	"bữa cơm nhà và món mẹ nấu",
	"âm nhạc đang nghe dạo gần đây",
}

// buildPrompt assembles the Gemini prompt for one post.
// recent contains openings of the bot's latest posts so the model avoids
// repeating itself.
func buildPrompt(p persona, topic string, recent []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `Bạn là %q, một người dùng mạng xã hội Việt Nam. Phong cách viết: %s.

Hãy viết đúng MỘT bài đăng mạng xã hội bằng tiếng Việt về chủ đề: %s.

Yêu cầu:
- Dài 1-4 câu, tự nhiên như người thật viết, không mở đầu bằng lời chào.
- Không dùng hashtag trong phần nội dung.
- Kèm 1-3 tag ngắn (chỉ chữ thường không dấu, số, gạch dưới; ví dụ "caphe", "hanoi", "worklife").
`, p.displayName, p.style, topic)

	if len(recent) > 0 {
		b.WriteString("\nTránh lặp lại ý của các bài gần đây:\n")
		for _, r := range recent {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}

	b.WriteString("\nTrả về JSON: {\"content\": \"...\", \"tags\": [\"...\"]}")
	return b.String()
}
