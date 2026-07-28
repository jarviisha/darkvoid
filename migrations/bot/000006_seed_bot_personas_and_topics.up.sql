-- Seed the personas, topics, and defaults that cmd/bot used to carry as compile-time
-- values, so migrating a running deployment is behaviour-preserving: the bot finds
-- exactly the pool it had before, now editable through the admin API.
--
-- Ported verbatim from cmd/bot/personas.go (`personas`, `topics`) and
-- cmd/bot/config.go (`defaultGeminiModels`, BOT_ACCOUNTS, BOT_POST_INTERVAL).

INSERT INTO bot.config (id, post_interval_seconds, accounts, models)
VALUES (
    1,
    30,
    3,
    ARRAY['gemini-2.5-flash', 'gemini-2.5-flash-lite', 'gemini-2.0-flash', 'gemini-flash-lite-latest']
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO bot.bots (username, display_name, style) VALUES
    ('bot_sky',    'Sky Vũ',         'giọng trẻ trung, hài hước, hay dùng emoji, thỉnh thoảng chêm tiếng Anh kiểu dân văn phòng'),
    ('bot_mien',   'Miên Đặng',      'giọng trầm lắng, tản văn, viết về cảm xúc và quan sát đời thường, ít emoji'),
    ('bot_codek',  'Khoa Coder',     'giọng dev backend, kể chuyện nghề lập trình, tự trào, thích chia sẻ bài học kỹ thuật'),
    ('bot_hafood', 'Hà Foodie',      'giọng food blogger, mô tả món ăn sống động, hay rủ rê mọi người đi ăn'),
    ('bot_dulich', 'Phong Xê Dịch',  'giọng travel blogger, kể trải nghiệm những nơi vừa đi qua, giàu hình ảnh')
ON CONFLICT (username) DO NOTHING;

INSERT INTO bot.topics (content) VALUES
    ('một bug khó chịu vừa gặp khi code và bài học rút ra'),
    ('quán cà phê mới phát hiện ở góc phố quen'),
    ('một món ăn đường phố Việt Nam đáng nhớ'),
    ('chuyến đi cuối tuần đến một tỉnh miền núi phía Bắc'),
    ('suy nghĩ về work-life balance của dân văn phòng'),
    ('cuốn sách hoặc bộ phim vừa xem xong'),
    ('thời tiết hôm nay và tâm trạng đi kèm'),
    ('một thói quen nhỏ giúp ngày làm việc dễ thở hơn'),
    ('kỷ niệm thời sinh viên chợt nhớ lại'),
    ('trải nghiệm học một kỹ năng mới'),
    ('chuyện dở khóc dở cười khi đi làm'),
    ('một góc Sài Gòn hoặc Hà Nội lúc sáng sớm'),
    ('cảm nghĩ về mạng xã hội và thói quen lướt điện thoại'),
    ('bữa cơm nhà và món mẹ nấu'),
    ('âm nhạc đang nghe dạo gần đây')
ON CONFLICT (content) DO NOTHING;
