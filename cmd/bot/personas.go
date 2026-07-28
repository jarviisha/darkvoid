package main

import (
	"fmt"
	"strings"
)

// persona is one bot account and the writing voice Gemini should adopt, as handed
// out by GET /bot/plan. It used to be a compile-time slice here; moving it into the
// database is what lets an operator retire or re-voice a bot without a rebuild.
type persona struct {
	// id is the bot.bots row, echoed back when reporting a run.
	id          string
	username    string
	displayName string
	style       string
	// runRequested means an operator asked this persona to post now, out of band
	// from the interval.
	runRequested bool
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
