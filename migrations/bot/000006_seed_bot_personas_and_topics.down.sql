-- Only the seeded rows are removed; personas and topics an admin added later are
-- left alone. bot.runs rows for a deleted persona go with it via ON DELETE CASCADE.

DELETE FROM bot.bots WHERE username IN ('bot_sky', 'bot_mien', 'bot_codek', 'bot_hafood', 'bot_dulich');

DELETE FROM bot.topics WHERE content IN (
    'một bug khó chịu vừa gặp khi code và bài học rút ra',
    'quán cà phê mới phát hiện ở góc phố quen',
    'một món ăn đường phố Việt Nam đáng nhớ',
    'chuyến đi cuối tuần đến một tỉnh miền núi phía Bắc',
    'suy nghĩ về work-life balance của dân văn phòng',
    'cuốn sách hoặc bộ phim vừa xem xong',
    'thời tiết hôm nay và tâm trạng đi kèm',
    'một thói quen nhỏ giúp ngày làm việc dễ thở hơn',
    'kỷ niệm thời sinh viên chợt nhớ lại',
    'trải nghiệm học một kỹ năng mới',
    'chuyện dở khóc dở cười khi đi làm',
    'một góc Sài Gòn hoặc Hà Nội lúc sáng sớm',
    'cảm nghĩ về mạng xã hội và thói quen lướt điện thoại',
    'bữa cơm nhà và món mẹ nấu',
    'âm nhạc đang nghe dạo gần đây'
);

DELETE FROM bot.config WHERE id = 1;
