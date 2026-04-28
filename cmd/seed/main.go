// cmd/seed/main.go — Sandbox data seeder
//
// Usage:
//
//	go run ./cmd/seed [--posts=500] [--likes-per-post=40] [--comments-per-post=5] [--reset]
//
// Flags:
//
//	--posts              number of posts to create (default 500)
//	--likes-per-post     max likes per post, randomised 0..N (default 40)
//	--comments-per-post  max comments per post, randomised 0..N (default 5)
//	--reset              truncate all seed data before running
//
// The seeder always creates the same 20 realistic Vietnamese user personas.
// Posts are distributed across users with natural Vietnamese social media content.
// Follow graph uses a power-law distribution: ~4 popular users get followed by 60%+ of others.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	postentity "github.com/jarviisha/darkvoid/internal/feature/post/entity"
	postrepo "github.com/jarviisha/darkvoid/internal/feature/post/repository"
	postservice "github.com/jarviisha/darkvoid/internal/feature/post/service"
	userrepo "github.com/jarviisha/darkvoid/internal/feature/user/repository"
	"github.com/jarviisha/darkvoid/pkg/codohue"
	"github.com/jarviisha/darkvoid/pkg/config"
	"github.com/jarviisha/darkvoid/pkg/database"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/jarviisha/darkvoid/pkg/tfidf"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	posts := flag.Int("posts", 500, "number of posts to create")
	likesPerPost := flag.Int("likes-per-post", 40, "max likes per post (randomised 0..N)")
	commentsPerPost := flag.Int("comments-per-post", 5, "max comments per post (randomised 0..N)")
	reset := flag.Bool("reset", false, "truncate seed data before seeding")
	flag.Parse()

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	pool, err := database.NewPostgresPool(ctx, &database.Config{
		Host:            cfg.Database.Host,
		Port:            cfg.Database.Port,
		User:            cfg.Database.User,
		Password:        cfg.Database.Password,
		Database:        cfg.Database.Database,
		SSLMode:         cfg.Database.SSLMode,
		MaxConns:        cfg.Database.MaxConns,
		MinConns:        cfg.Database.MinConns,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
	})
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	if err = database.HealthCheck(ctx, pool); err != nil {
		log.Fatalf("db health check: %v", err)
	}

	services, cleanupServices, err := newSeedServices(ctx, pool, cfg)
	if err != nil {
		log.Fatalf("seed services: %v", err)
	}
	defer cleanupServices()

	s := &seeder{
		pool:     pool,
		services: services,
		rng:      rand.New(rand.NewSource(42)), //nolint:gosec // fixed seed for reproducible data
	}

	if *reset {
		log.Println("resetting seed data...")
		s.reset(ctx)
	}

	log.Println("seeding users...")
	userIDs, popularIDs, err := s.seedUsers(ctx)
	if err != nil {
		log.Fatalf("seed users: %v", err)
	}
	log.Printf("  created %d users (%d popular)", len(userIDs), len(popularIDs))

	log.Println("seeding follows (power-law graph)...")
	if err = s.seedFollows(ctx, userIDs, popularIDs); err != nil {
		log.Fatalf("seed follows: %v", err)
	}

	log.Printf("seeding %d posts...", *posts)
	postIDs, err := s.seedPosts(ctx, userIDs, *posts)
	if err != nil {
		log.Fatalf("seed posts: %v", err)
	}
	log.Printf("  created %d posts", len(postIDs))

	log.Println("seeding likes...")
	if err = s.seedLikes(ctx, userIDs, postIDs, *likesPerPost); err != nil {
		log.Fatalf("seed likes: %v", err)
	}

	log.Printf("seeding comments (max %d/post)...", *commentsPerPost)
	if err = s.seedComments(ctx, userIDs, postIDs, *commentsPerPost); err != nil {
		log.Fatalf("seed comments: %v", err)
	}

	log.Println("done.")
}

// ── personas ─────────────────────────────────────────────────────────────────

type persona struct {
	username    string
	displayName string
	bio         string
	location    string
	popular     bool // gets followed by most other users
}

// personas is the fixed set of 20 realistic Vietnamese users.
// All accounts share password "Seed@12345".
var personas = []persona{
	{
		username:    "minh.nguyen.dev",
		displayName: "Minh Nguyễn",
		bio:         "Backend engineer @ một startup nào đó 🖥️ Golang, PostgreSQL. Hay than thở về distributed systems.",
		location:    "Hà Nội",
		popular:     true,
	},
	{
		username:    "linh.tran.design",
		displayName: "Linh Trần",
		bio:         "UI/UX designer • Yêu cà phê và Figma như nhau ☕ Thích ngồi quán vừa sketch vừa nghe nhạc.",
		location:    "TP.HCM",
		popular:     true,
	},
	{
		username:    "huy.photo",
		displayName: "Huy Phạm",
		bio:         "Travel & street photographer 📷 Đã đặt chân đến 30+ tỉnh thành. Đà Nẵng là nhà.",
		location:    "Đà Nẵng",
		popular:     true,
	},
	{
		username:    "thu.foodie",
		displayName: "Thu Lê",
		bio:         "Ăn là lẽ sống 🍜 Food blogger fulltime. Review nhà hàng, quán vỉa hè, từ bình dân đến sang chảnh.",
		location:    "TP.HCM",
		popular:     true,
	},
	{
		username:    "nam.startup",
		displayName: "Nam Hoàng",
		bio:         "Co-founder @ Dẫu Vậy. Đang xây dựng thứ gì đó điên rồ. Thất bại nhiều, học được nhiều hơn.",
		location:    "Hà Nội",
	},
	{
		username:    "mai.do.hust",
		displayName: "Mai Đỗ",
		bio:         "Sinh viên BKHN năm 3 🎓 Đang học ML và hay hỏi những câu ngớ ngẩn trên Stack Overflow.",
		location:    "Hà Nội",
	},
	{
		username:    "khanh.devops",
		displayName: "Khánh Vũ",
		bio:         "DevOps / SRE. Kubernetes ban ngày, đánh guitar ban đêm 🎸 Obsessed với observability.",
		location:    "TP.HCM",
	},
	{
		username:    "an.hoian",
		displayName: "An Bùi",
		bio:         "Content creator, Hội An 🏮 Viết về lối sống chậm, cà phê sáng và những khoảnh khắc bình yên.",
		location:    "Hội An",
	},
	{
		username:    "tuan.pm",
		displayName: "Tuấn Nguyễn",
		bio:         "Product Manager • từng là dev, giờ viết spec thay vì code 😅 Yêu thích data-driven decisions.",
		location:    "Hà Nội",
	},
	{
		username:    "linh.freelance",
		displayName: "Linh Phạm",
		bio:         "Freelance photographer + digital nomad 🌏 Tháng này Sài Gòn, tháng sau chưa biết.",
		location:    "TP.HCM",
	},
	{
		username:    "duc.backend",
		displayName: "Đức Trương",
		bio:         "Senior dev @ TikiNow. Gopher thuần. Không thích microservices nhưng vẫn phải dùng 😂",
		location:    "Hà Nội",
	},
	{
		username:    "van.teacher",
		displayName: "Vân Ngô",
		bio:         "Giáo viên tiếng Anh 📚 Mê đọc sách, thích viết, hay trích dẫn những câu hay ho.",
		location:    "Huế",
	},
	{
		username:    "hung.gamer",
		displayName: "Hùng Đinh",
		bio:         "Full-stack dev ban ngày, gamer ban đêm 🎮 React + Node. Đang học Rust cho vui.",
		location:    "TP.HCM",
	},
	{
		username:    "phuong.art",
		displayName: "Phương Lý",
		bio:         "Illustrator & graphic designer ✏️ Vẽ theo cảm xúc. Nhận order commission khi hứng.",
		location:    "Hà Nội",
	},
	{
		username:    "long.coffee",
		displayName: "Long Bùi",
		bio:         "Barista → Product designer. Cà phê và code là hai thứ giúp tôi tồn tại.",
		location:    "Đà Lạt",
	},
	{
		username:    "ha.researcher",
		displayName: "Hà Đinh",
		bio:         "NLP researcher 🔬 PhD student. Hay viết về AI, ngôn ngữ học và những điều AI chưa hiểu được.",
		location:    "Hà Nội",
	},
	{
		username:    "bao.cyclist",
		displayName: "Bảo Trần",
		bio:         "Software engineer + cycling enthusiast 🚴 Đạp xe từ HN đến HCM là dream bucket list.",
		location:    "Hà Nội",
	},
	{
		username:    "yen.writer",
		displayName: "Yến Phạm",
		bio:         "Viết tản văn, thơ lãng đãng 🌙 Hay đăng lúc 2 giờ sáng khi không ngủ được.",
		location:    "TP.HCM",
	},
	{
		username:    "quang.data",
		displayName: "Quang Lê",
		bio:         "Data engineer @ VNG. Pipeline, Spark, Kafka. Tin rằng data không bao giờ sạch.",
		location:    "TP.HCM",
	},
	{
		username:    "thi.student",
		displayName: "Thị Nguyễn",
		bio:         "UEH K2021 🎓 Đang thực tập, đang học, đang cố. Thích chụp ảnh phố xá Sài Gòn.",
		location:    "TP.HCM",
	},
}

// ── post content ─────────────────────────────────────────────────────────────

// postTemplates is a bank of realistic Vietnamese social media posts grouped by category.
var postTemplates = []string{
	// Tech / coding
	"Hôm nay debug mãi mới tìm ra bug là do timezone. Một lần nữa UTC lại cứu cả team 😅 Nhớ mãi bài học này.",
	"Vừa deploy lên production xong, nhìn metrics ổn định là thở phào nhẹ nhõm hơn bất kỳ thứ gì 📊",
	"Golang goroutine leak nếu không handle context cẩn thận. Mất 2 ngày debug mới hiểu ra. Chia sẻ để mọi người tránh.",
	"Code review mà không có test thì chỉ là đọc văn xuôi thôi. Team mình giờ enforce 80% coverage, ngại nhưng cần thiết.",
	"Microservices không phải silver bullet. Đôi khi một monolith tốt hơn 10 service lộn xộn. Đừng over-engineer.",
	"Kubernetes hôm nay lại autoscale nhầm. Thứ 6 mà, thôi để thứ 2 fix 😭",
	"Mấy hôm nay đang refactor codebase cũ, comment toàn là TODO từ 3 năm trước. Không ai quay lại fix cả 😂",
	"Lần đầu implement rate limiting từ scratch thay vì dùng library. Học được nhiều thứ về Redis và sliding window.",
	"SQL query chạy 3 giây, thêm index là còn 50ms. Cái cảm giác optimization thành công thật sự rất đã 🚀",
	"Đọc lại code mình viết 2 năm trước, không hiểu tại sao ngày đó lại viết như vậy. Growth hay là quên? 🤔",
	"Vừa đọc xong 'Designing Data-Intensive Applications'. Nếu bạn làm backend mà chưa đọc, đọc ngay đi.",
	"Meeting 2 tiếng không có outcome. Lẽ ra có thể là một cái Slack message.",
	"Remote work nghe hay nhưng blur giữa work và life thật sự khó. Đang tìm lại cân bằng.",
	"PR mở 2 tuần không ai review. Đây là lý do mình hay review PR người khác trước khi đòi review lại 😅",
	"PostgreSQL full-text search underrated. Nhiều case không cần Elasticsearch đâu, Postgres làm được hết.",

	// Travel / places
	"Lần đầu đến Hà Giang, không hiểu sao nước mình lại đẹp đến vậy. Cao nguyên đá mùa hoa tam giác mạch là ảo ma nhất.",
	"Sáng sớm ở Hội An, trước khi khách du lịch đến, phố cổ có một sự yên tĩnh rất lạ và đẹp.",
	"Đà Lạt tháng 12 lạnh thật sự rồi. Sương mù buổi sáng, cà phê nóng, không muốn về.",
	"Phú Quốc giờ đông quá. Nhớ cái ngày cách đây 5 năm chỉ có dân địa phương và ít khách lạc vào.",
	"Ninh Bình sau mưa là một bức tranh khác. Nước sông Ngô Đồng xanh ngắt, núi đá mờ trong sương.",
	"Mộc Châu tháng 10, hoa cải vàng nở rộ, cảm giác như đang ở đâu đó ngoài Việt Nam.",
	"Bờ biển Lăng Cô vắng người vào mùa này, nước trong đến mức thấy đáy rõ ràng 🌊",
	"Train từ Hà Nội đến Lào Cai ban đêm, nằm giường tầng nhìn sao, cảm giác road trip kiểu Việt Nam.",

	// Food
	"Phở gà Hàng Trống sáng sớm, nước dùng trong vắt, rau thơm tươi, không có gì ngon hơn cái này lúc 7 giờ sáng 🍜",
	"Bún bò Huế ở Sài Gòn không bao giờ ngon bằng ở Huế. Lý giải thế nào đi nữa cũng thấy đúng.",
	"Cơm tấm Sài Gòn 2 giờ sáng sau khi cà phê với bạn bè. Tô sườn bì chả nóng hổi là ký ức.",
	"Mì Quảng Đà Nẵng ăn với bánh tráng nướng, thêm ít rau. Người Đà Nẵng không thể sống thiếu món này.",
	"Bánh mì Sài Gòn là level khác. Ra nước ngoài ăn bánh mì mới hiểu sao mình hay nhớ nhà.",
	"Bún đậu mắm tôm Hà Nội — thứ mà người Sài Gòn thường ngại nhưng khi quen rồi thì ghiền kinh khủng.",
	"Ốc luộc vỉa hè tối thứ 6, mấy đứa bạn ngồi nhậu nhẹt, kể chuyện trên đời. Simple và perfect.",
	"Chả cá Lã Vọng lần đầu ăn cứ tưởng chỉ là cá chiên, nhưng cái mùi thì và mắm tôm làm mình hiểu tại sao nổi tiếng.",

	// Daily life / thoughts
	"Mấy hôm nay cứ 2 giờ sáng mới ngủ được. Không phải lo lắng, chỉ là không ngủ được. Ai có kinh nghiệm không?",
	"Cuối tuần này mình tắt hết notification và đọc sách. Cảm giác detox digital thật sự cần thiết.",
	"Năm 25 tuổi mình sợ thất bại. 28 tuổi hiểu ra thất bại dạy nhiều hơn thành công. Perspective thật sự thay đổi.",
	"Người Hà Nội và người Sài Gòn giống nhau ở chỗ đều than thở về thời tiết thành phố mình nhưng không chịu đi nơi khác.",
	"Hôm nay trời mưa cả ngày ở Hà Nội. Loại mưa phùn mùa đông, ngồi trong quán cà phê uống trà đào thấy sống là đẹp.",
	"Mình hay hỏi 'nếu ngày đó làm khác đi thì sao' — rồi nhận ra câu hỏi đó vô nghĩa. Đang focus vào hiện tại hơn.",
	"Nói chuyện với một bạn intern hôm nay, nhớ lại cái hồi đó mình cũng naïve và đầy nhiệt huyết như vậy.",
	"Sài Gòn mưa chiều là một loại nghệ thuật. Ướt hết nhưng ai cũng cười vì không tránh kịp.",
	"Đọc lại nhật ký 5 năm trước, thấy bản thân đó và bây giờ như hai người khác nhau. Cả tệ hơn lẫn tốt hơn.",
	"Cứ nghĩ remote work là dream, giờ thỉnh thoảng nhớ tiếng ồn của văn phòng, nhớ coffee machine, nhớ đồng nghiệp.",

	// Creative / misc
	"Vừa nghe lại album cũ từ 2015. Âm nhạc có khả năng teleport bạn về một thời điểm cụ thể không thể giải thích được.",
	"Đang học vẽ, sau 3 tháng tay vẫn run khi cầm bút, nhưng không còn sợ tờ giấy trắng nữa. Progress.",
	"Quyển sách hay nhất mình đọc năm nay không phải tech book. Là 'Cách chúng ta tư duy' của John Dewey.",
	"Photography dạy mình nhìn ánh sáng khác đi. Giờ đi đâu cũng để ý golden hour, shadow, texture.",
	"Viết là cách tốt nhất để hiểu mình đang nghĩ gì. Không nhất thiết phải đăng lên, viết cho bản thân cũng được.",
	"Không có gì trị liệu hơn một chuyến đi xe máy một mình trong thành phố lúc khuya. Hà Nội 1 giờ sáng rất lạ và đẹp.",
	"Bạn bè từ hồi đại học, 5 năm mỗi đứa một phương, meet up lại vẫn như chưa xa cách ngày nào.",
	"Cái hay của side project là không có deadline, không có pressure. Làm vì thích, dừng vì không hứng.",

	// Opinions / insights
	"Học ngoại ngữ thứ ba mới hiểu sao mẹ đẻ lại ảnh hưởng cách mình nghĩ sâu đến vậy.",
	"Productivity không phải làm nhiều, là làm đúng thứ cần làm. Một ngày 3 tasks quan trọng tốt hơn 20 tasks vặt.",
	"LinkedIn đang ngày càng giống Facebook nhưng mọi người post về công việc. Không chắc đây là tốt hay xấu.",
	"Nếu bạn không thể giải thích concept đơn giản cho người không biết gì, bạn chưa thật sự hiểu nó.",
	"Junior dev hay overthink về career path. Senior dev hay overthink về work-life balance. Mỗi giai đoạn có nỗi lo riêng.",
}

// commentTemplates is a bank of realistic Vietnamese social media comments.
var commentTemplates = []string{
	"Đồng ý với bạn 100% 👍",
	"Hay quá, share cho bạn bè đọc luôn!",
	"Cảm ơn bạn đã chia sẻ, mình học được nhiều thứ.",
	"Đúng vl luôn, mình cũng gặp y chang vụ này 😂",
	"Bạn viết hay thật, đọc mà thấy vào lòng.",
	"Ủa mình cũng đang gặp vấn đề tương tự, bạn giải quyết sao rồi?",
	"Lần sau cho mình đi với nhé! 🙋",
	"Nhìn ảnh mà thèm quá rồi, mai phải đi ăn thôi.",
	"Đây là lần thứ 3 mình đọc lại bài này 😅",
	"Thật ra mình thấy perspective khác nhỉ, nhưng vẫn hiểu ý bạn muốn nói.",
	"Đi đâu vậy bạn ơi? Xinh quá!",
	"Mình bookmark cái này rồi, hữu ích lắm.",
	"Haha đúng là vậy đó, ai làm dev cũng hiểu cảm giác này 😭",
	"Bạn ơi recommend quán không? Nhìn ngon ghê.",
	"Relate quá trời 😭 y hệt mình luôn.",
	"Ồ mình không biết điều này, cảm ơn bạn!",
	"Bài viết rất hay, mong bạn viết thêm chủ đề này.",
	"Hà Nội hay Sài Gòn thế bạn?",
	"Mình cũng vừa đọc xong quyển đó, đồng cảm ghê.",
	"Bao giờ mình mới được đến đây một lần nhỉ 😍",
	"+1 cho ý kiến này, rất đúng.",
	"Thật ra mình nghĩ vấn đề còn phức tạp hơn thế, nhưng thôi.",
	"Bạn chụp ảnh đẹp quá, preset gì vậy?",
	"Ước gì có thể làm vậy 😪",
	"Follow bạn từ lâu nhưng hôm nay mới comment lần đầu 🙈",
	"Lần nào đọc bài bạn cũng học được gì đó mới.",
	"Mình đã nghĩ tới điều này nhưng không diễn đạt được. Bạn nói đúng hộ rồi.",
	"Giờ mình mới hiểu tại sao nó lại như vậy, cảm ơn!",
	"Sẽ thử áp dụng cách này xem sao 🤞",
	"Hay đó, lần sau rảnh thì viết thêm đi bạn.",
}

// hashtagPool defines hashtag names grouped by category.
// Each post randomly picks tags from one category.
var hashtagPool = [][]string{
	{"golang", "backend", "programming", "techvn"},
	{"devops", "kubernetes", "cloud", "techvn"},
	{"design", "uxdesign", "figma", "productdesign"},
	{"travel", "vietnam", "dulich", "khampha"},
	{"photography", "streetphotography", "landscape"},
	{"food", "hanoi", "saigon", "amthuc"},
	{"coffee", "dalat", "cafehopping"},
	{"startup", "entrepreneurship", "buildinpublic"},
	{"life", "mentalhealth", "productivity"},
	{"machinelearning", "ai", "nlp", "datascience"},
}

// ── seeder ────────────────────────────────────────────────────────────────────

type seeder struct {
	pool     *pgxpool.Pool
	services *seedServices
	rng      *rand.Rand
}

type seedServices struct {
	post    *postservice.PostService
	like    *postservice.LikeService
	comment *postservice.CommentService
}

type seedUserReader struct {
	userRepo *userrepo.UserRepository
}

func (r *seedUserReader) GetAuthorsByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*postentity.Author, error) {
	users, err := r.userRepo.GetUsersByIDsAny(ctx, ids)
	if err != nil {
		return nil, err
	}

	authors := make(map[uuid.UUID]*postentity.Author, len(users))
	for _, u := range users {
		authors[u.ID] = &postentity.Author{
			ID:          u.ID,
			Username:    u.Username,
			DisplayName: u.DisplayName,
			AvatarKey:   u.AvatarKey,
		}
	}
	return authors, nil
}

func newSeedServices(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) (*seedServices, func(), error) {
	userRepository := userrepo.NewUserRepository(pool)
	userReader := &seedUserReader{userRepo: userRepository}

	postRepository := postrepo.NewPostRepository(pool)
	mediaRepository := postrepo.NewMediaRepository(pool)
	likeRepository := postrepo.NewLikeRepository(pool)
	commentRepository := postrepo.NewCommentRepository(pool)
	commentMediaRepository := postrepo.NewCommentMediaRepository(pool)
	commentLikeRepository := postrepo.NewCommentLikeRepository(pool)
	hashtagRepository := postrepo.NewHashtagRepository(pool)
	mentionRepository := postrepo.NewMentionRepository(pool)
	commentMentionRepository := postrepo.NewCommentMentionRepository(pool)

	postSvc := postservice.NewPostService(pool, postRepository, mediaRepository, userReader, hashtagRepository,
		postservice.WithLikeRepo(likeRepository),
		postservice.WithMentionRepo(mentionRepository),
	)
	likeSvc := postservice.NewLikeService(likeRepository, postRepository)
	commentSvc := postservice.NewCommentService(pool, commentRepository, commentMediaRepository, postRepository, userReader,
		postservice.WithCommentLikeRepo(commentLikeRepository),
		postservice.WithCommentMentionRepo(commentMentionRepository),
	)

	var redisClient *pkgredis.Client
	cleanup := func() {}
	if cfg.Codohue.Enabled {
		if !cfg.Redis.Enabled {
			return nil, cleanup, fmt.Errorf("CODOHUE_ENABLED=true requires REDIS_ENABLED=true so seed behavior events can be published")
		}

		var err error
		redisClient, err = pkgredis.New(ctx, &pkgredis.Config{
			Host:     cfg.Redis.Host,
			Port:     cfg.Redis.Port,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
			PoolSize: cfg.Redis.PoolSize,
		})
		if err != nil {
			return nil, cleanup, fmt.Errorf("connect redis for codohue events: %w", err)
		}
		cleanup = func() {
			if err := redisClient.Close(); err != nil {
				log.Printf("  warn close redis: %v", err)
			}
		}

		codohueClient := codohue.NewClient(cfg.Codohue.BaseURL, cfg.Codohue.NamespaceKey, cfg.Codohue.Namespace, redisClient)
		if codohueClient == nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("create codohue client")
		}
		if err := codohueClient.Ping(ctx); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("codohue ping: %w", err)
		}

		postSvc.WithEmbedding(tfidf.New(cfg.Codohue.EmbeddingDim), codohueClient)
		likeSvc.WithBehaviorEventPublisher(codohueClient)
		commentSvc.WithBehaviorEventPublisher(codohueClient)
		log.Printf("codohue wired for seed events and post embeddings (namespace=%s)", cfg.Codohue.Namespace)
	}

	return &seedServices{
		post:    postSvc,
		like:    likeSvc,
		comment: commentSvc,
	}, cleanup, nil
}

func (s *seeder) reset(ctx context.Context) {
	stmts := []string{
		`DELETE FROM post.post_hashtags`,
		`DELETE FROM post.hashtags`,
		`DELETE FROM post.comment_mentions`,
		`DELETE FROM post.post_mentions`,
		`DELETE FROM post.comment_likes`,
		`DELETE FROM post.comment_media`,
		`DELETE FROM post.comments`,
		`DELETE FROM post.likes`,
		`DELETE FROM post.post_media`,
		`DELETE FROM post.posts`,
		`DELETE FROM usr.follows`,
		`DELETE FROM usr.users WHERE email LIKE '%@seed.local'`,
	}
	for _, stmt := range stmts {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			log.Printf("  warn reset (%s): %v", stmt[:min(40, len(stmt))], err)
		}
	}
}

// seedUsers creates the fixed persona set and returns (allIDs, popularIDs).
func (s *seeder) seedUsers(ctx context.Context) ([]uuid.UUID, []uuid.UUID, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte("Seed@12345"), 12)
	if err != nil {
		return nil, nil, fmt.Errorf("hash password: %w", err)
	}
	pwHash := string(hash)

	allIDs := make([]uuid.UUID, 0, len(personas))
	popularIDs := make([]uuid.UUID, 0, 4)

	for _, p := range personas {
		id := uuid.New()
		email := fmt.Sprintf("%s@seed.local", p.username)

		var existingID uuid.UUID
		err := s.pool.QueryRow(ctx, `
			INSERT INTO usr.users (id, username, email, password_hash, is_active, display_name, bio, location)
			VALUES ($1, $2, $3, $4, true, $5, $6, $7)
			ON CONFLICT (email) DO UPDATE
				SET username=EXCLUDED.username, display_name=EXCLUDED.display_name,
				    bio=EXCLUDED.bio, location=EXCLUDED.location
			RETURNING id`,
			id, p.username, email, pwHash, p.displayName, p.bio, p.location,
		).Scan(&existingID)
		if err != nil {
			return nil, nil, fmt.Errorf("insert user %s: %w", p.username, err)
		}

		allIDs = append(allIDs, existingID)
		if p.popular {
			popularIDs = append(popularIDs, existingID)
		}
	}
	return allIDs, popularIDs, nil
}

// seedFollows builds a power-law follow graph:
//   - Popular users are followed by 70% of all other users.
//   - Non-popular users are followed by ~15% of others.
//   - Each user also follows 3–7 random other users (organic connections).
func (s *seeder) seedFollows(ctx context.Context, allIDs, popularIDs []uuid.UUID) error {
	popularSet := make(map[uuid.UUID]bool, len(popularIDs))
	for _, id := range popularIDs {
		popularSet[id] = true
	}

	insert := func(follower, followee uuid.UUID) error {
		if follower == followee {
			return nil
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO usr.follows (follower_id, followee_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			follower, followee,
		)
		return err
	}

	for _, followerID := range allIDs {
		for _, followeeID := range allIDs {
			if followerID == followeeID {
				continue
			}
			var threshold float32
			if popularSet[followeeID] {
				threshold = 0.70
			} else {
				threshold = 0.15
			}
			if s.rng.Float32() < threshold {
				if err := insert(followerID, followeeID); err != nil {
					return err
				}
			}
		}

		// Each user organically follows a few random peers.
		n := 3 + s.rng.Intn(5)
		perm := s.rng.Perm(len(allIDs))
		for _, idx := range perm[:min(n, len(perm))] {
			if err := insert(followerID, allIDs[idx]); err != nil {
				return err
			}
		}
	}
	return nil
}

// seedPosts creates n posts distributed across users.
// Post ages are spread over the past 21 days; recent posts are more frequent
// to simulate realistic activity patterns.
func (s *seeder) seedPosts(ctx context.Context, userIDs []uuid.UUID, n int) ([]uuid.UUID, error) {
	visibilities := []postentity.Visibility{
		postentity.VisibilityPublic,
		postentity.VisibilityPublic,
		postentity.VisibilityPublic,
		postentity.VisibilityFollowers,
	} // 75% public
	ids := make([]uuid.UUID, 0, n)

	for i := range n {
		authorID := userIDs[s.rng.Intn(len(userIDs))]
		visibility := visibilities[s.rng.Intn(len(visibilities))]
		content := postTemplates[s.rng.Intn(len(postTemplates))]
		tags := s.pickTags()

		// Bias toward recent posts: exponential distribution capped at 21 days.
		hoursAgo := s.rng.ExpFloat64() * 48 // mean = 2 days
		if hoursAgo > 21*24 {
			hoursAgo = float64(s.rng.Intn(21*24)) + 1
		}
		createdAt := time.Now().Add(-time.Duration(hoursAgo * float64(time.Hour)))

		p, err := s.services.post.CreatePost(ctx, authorID, content, visibility, nil, nil, tags)
		if err != nil {
			return nil, fmt.Errorf("create post %d: %w", i, err)
		}
		if _, err = s.pool.Exec(ctx, `
			UPDATE post.posts
			SET created_at = $2, updated_at = $2
			WHERE id = $1`,
			p.ID, createdAt,
		); err != nil {
			return nil, fmt.Errorf("backdate post %d: %w", i, err)
		}
		ids = append(ids, p.ID)
	}
	return ids, nil
}

// pickTags gives each post a 60% chance of receiving 1-3 category tags.
func (s *seeder) pickTags() []string {
	if s.rng.Float32() > 0.60 {
		return nil
	}

	group := hashtagPool[s.rng.Intn(len(hashtagPool))]
	n := 1 + s.rng.Intn(min(3, len(group)))
	perm := s.rng.Perm(len(group))
	tags := make([]string, 0, n)
	for _, idx := range perm[:n] {
		tags = append(tags, group[idx])
	}
	return tags
}

// seedLikes distributes likes realistically:
// recent posts and posts by popular users get more likes.
func (s *seeder) seedLikes(ctx context.Context, userIDs, postIDs []uuid.UUID, maxLikes int) error {
	for i, postID := range postIDs {
		// Recent posts (first third of slice, created most recently) get more likes.
		multiplier := 1.0
		if i < len(postIDs)/3 {
			multiplier = 2.0
		}
		n := min(int(float64(s.rng.Intn(maxLikes+1))*multiplier), len(userIDs))

		perm := s.rng.Perm(len(userIDs))
		for _, idx := range perm[:n] {
			liked, err := s.services.like.Toggle(ctx, userIDs[idx], postID)
			if err != nil {
				return fmt.Errorf("toggle like: %w", err)
			}
			if !liked {
				return fmt.Errorf("toggle like unexpectedly unliked post %s for user %s", postID, userIDs[idx])
			}
		}
	}
	return nil
}

// seedComments adds top-level comments and occasional replies to posts.
func (s *seeder) seedComments(ctx context.Context, userIDs, postIDs []uuid.UUID, maxPerPost int) error {
	for _, postID := range postIDs {
		n := s.rng.Intn(maxPerPost + 1)
		commentIDs := make([]uuid.UUID, 0, n)

		for range n {
			authorID := userIDs[s.rng.Intn(len(userIDs))]
			content := commentTemplates[s.rng.Intn(len(commentTemplates))]

			var parentID *uuid.UUID
			// 25% chance of being a reply to an existing comment.
			if len(commentIDs) > 0 && s.rng.Float32() < 0.25 {
				parent := commentIDs[s.rng.Intn(len(commentIDs))]
				parentID = &parent
			}

			comment, err := s.services.comment.CreateComment(ctx, postID, authorID, parentID, content, nil, nil)
			if err != nil {
				return fmt.Errorf("create comment: %w", err)
			}
			if parentID == nil {
				commentIDs = append(commentIDs, comment.ID)
			}
		}
	}
	return nil
}
