# Design: Materialized Ranked Feed (codohue-powered)

> Trạng thái: **DRAFT v2 — đã qua vòng review 1 (2026-07-24)**. Vòng review đã
> sửa 3 lỗ hổng: (1) score fan-out là hằng số → thêm encoding nhúng `createdAt`,
> (2) `AddPostsBatch` dùng `NX` nên không ghi đè được score → thêm
> `SetPostsBatch`, (3) thiếu kế hoạch migration → thêm §5 (key versioning).
> Đồng thời chốt 2 quyết định review (cursor "mềm", blend cộng trọng số — xem
> §11). Còn mở: Q3 (trigger), Q4 (candidate scope) — không chặn P1.
>
> Mục tiêu tài liệu: chốt kiến trúc trước khi động vào code, vì đây là thay đổi
> lớn ở feed subsystem. Khi chốt xong sẽ chuyển sang `plan.md`/`tasks.md` theo
> chuẩn repo.

## 1. Bối cảnh & vấn đề

Feed hiện tại (`internal/feature/feed`) đã có precomputed timeline, nhưng nó chỉ
materialize **membership theo thời gian**, chưa materialize **ranking**:

- **Background (fan-out / refresher)**: khi có post mới hoặc timeline miss,
  ghi `TimelineEntry{PostID, Score}` vào Redis ZSET với
  `Score = TimelineScoreFromTime(createdAt)` — tức **timestamp micro giây**,
  không phải điểm rank.
- **Hot path (`getFeedFromTimeline`)**: mỗi request vẫn phải
  1. `ReadPage` từ Redis (nhẹ) →
  2. `GetPostsByIDs` hydrate từ Postgres (I/O) →
  3. `rankCandidates` → `ranker.RankPosts` — **chấm điểm local scorer realtime** →
  4. enrich `isLiked` / `isFollowingAuthor` (query DB).

Hệ quả:
- Ranking chạy lại **mỗi request**, không tái sử dụng.
- Codohue CF (khi timeline miss, tầng "mixed candidates") được gọi **realtime
  trong hot path** → latency phụ thuộc codohue còn sống hay không tại thời điểm
  request.
- Không có chỗ tự nhiên để cắm codohue **Rankings** (re-rank theo CF) mà không
  làm nặng hot path.

## 2. Mục tiêu / Phi mục tiêu

**Mục tiêu**
- Chuyển toàn bộ **ranking (local + codohue CF) sang background**.
- Hot path chỉ còn: `ZREVRANGE` timeline (đã rank sẵn) → hydrate → enrich → trả.
  Không rank realtime, không gọi codohue realtime.
- Codohue Rankings là re-ranker chạy trong background job; **local scorer là
  fallback/floor** khi codohue trả điểm 0, lỗi, hoặc timeout.
- Giữ khả năng tắt hoàn toàn codohue (degrade về local-only) không đổi hot path.

**Phi mục tiêu (không làm ở vòng này)**
- Không tự sinh VIEW/SHARE events (việc riêng, là *tiền đề dữ liệu* — xem §10).
- Không đổi discover fallback cho user chưa follow ai (giữ nguyên local).
- Không đụng catalog/embedding pipeline (đã xong).

## 3. Kiến trúc đề xuất — "Lambda" (fast write + batch re-rank)

Cốt lõi: **điểm số trong timeline ZSET đổi từ timestamp → rank score**, và có
hai đường ghi điểm bổ sung cho nhau:

```
                         ┌─────────────────────────── HOT PATH (đọc) ──────────┐
                         │  GetFeed → ReadPage (Redis, đã rank) → hydrate DB    │
                         │           → enrich isLiked/isFollowing → trả 20 item │
                         └──────────────────────────────────────────────────────┘
                                        ▲ chỉ đọc, không rank, không codohue realtime
                                        │
        ┌───────────────────────────────┴───────────────────────────────┐
        │                        TIMELINE ZSET (Redis)                    │
        │        member = postID, score = RANK SCORE (đã tính sẵn)        │
        └───────────────────────────────▲───────────────────────────────┘
                    ghi nhanh (recency)  │        ghi lại theo CF (batch)
        ┌───────────────────────────────┴──────┐   ┌────────────────────┴─────────────┐
        │ (A) FAN-OUT ON WRITE                   │   │ (B) BACKGROUND RE-RANK JOB        │
        │  post mới → đẩy vào timeline followers │   │  định kỳ / theo trigger cho user  │
        │  score = local score (recency-heavy)   │   │  active:                          │
        │  → hiện NGAY, chưa cần CF               │   │   1. lấy candidate của user       │
        └────────────────────────────────────────┘   │   2. codohue Rankings re-rank     │
                                                      │   3. fallback local cho item CF=0 │
                                                      │   4. ghi đè score vào ZSET        │
                                                      └───────────────────────────────────┘
```

- **(A) Fan-out on write** — giữ tính realtime: post mới xuất hiện ngay với điểm
  tạm (local/recency). Không thể tính CF ở đây vì post mới chưa có interaction.
- **(B) Background re-rank job** — nơi **codohue Rankings** thực sự phát huy: lấy
  toàn bộ candidate timeline của một user, gọi codohue re-rank theo CF, ghi đè
  score (qua `SetPostsBatch` — xem §4.7). Local scorer là fallback cho từng item
  codohue không biết (score 0) hoặc khi cả call lỗi/timeout.

→ Hot path đọc ZSET đã rank sẵn. Local vs codohue "hòa giải" ở background, đúng
như kết luận các lượt trước: **local là floor luôn tồn tại, codohue là lớp phủ
khi có data.**

## 4. Thay đổi chi tiết theo thành phần

### 4.1 Data model — encoding `TimelineEntry.Score` (rank + createdAt packed)

- Hiện: `Score int64` = `UnixMicro(createdAt)`.
- Đổi ý nghĩa: `Score` = **packed(rank score, createdAt)**. KHÔNG dùng
  `int64(rankScore * 1e6)` thuần như bản draft v1, vì lỗi sau:

> **Vấn đề tie hàng loạt ở fan-out**: local score tại thời điểm ghi của post
> mới là **hằng số** — `engagement = log1p(0)*10 = 0` (post mới chưa có like),
> `recency = RecencyScale = 20` (tuổi ≈ 0h với mọi post), `bonus =
> RelationshipBonus = 10` (mọi entry trong timeline đều là following). Tức mọi
> post fan-out đều ghi đúng `30.0` → tie toàn bộ. Khi tie, thứ tự rơi về so
> sánh `postID` — mà post ID là `gen_random_uuid()` (UUIDv4 ngẫu nhiên, xem
> `migrations/post/000002`) → bài mới hiện theo **thứ tự ngẫu nhiên** thay vì
> mới-nhất-trước giữa 2 lần re-rank.

**Encoding đề xuất** — pack rank vào bit cao, createdAt vào bit thấp:

```go
// timeline.go — thay thế TimelineScoreFromTime
const (
    timelineEpoch      = 1577836800 // 2020-01-01T00:00:00Z; uint32 đủ tới ~2156
    timelineRankMax    = 1<<20 - 1  // clamp rank bucket vào 20 bit
    timelineRankScale  = 1000       // 3 chữ số thập phân của rank score
)

// PackTimelineScore encodes (rankScore, createdAt) into one sortable int64.
// Primary order: rank score (độ phân giải 0.001). Secondary: createdAt (giây).
// Tie-break cuối cùng: postID (đã có sẵn trong TimelinePosition/ReadPage).
func PackTimelineScore(rankScore float64, createdAt time.Time) int64 {
    // Round, không truncate: 10.001×1000 float ra 10000.999…8, truncate sẽ
    // rơi nhầm bucket và phá bảo đảm "hơn nhau 0.001 là thắng".
    bucket := int64(math.Round(rankScore * timelineRankScale))
    bucket = min(max(bucket, 0), timelineRankMax)
    ts := createdAt.UTC().Unix() - timelineEpoch
    ts = min(max(ts, 0), math.MaxUint32)
    return bucket<<32 | ts
}
```

Vì sao encoding này đúng:
- **So sánh đúng thứ tự**: bucket cao hơn → score cao hơn; cùng bucket (điển
  hình: khối post fan-out cùng điểm 30.0) → post mới hơn thắng. Giải quyết
  triệt để vấn đề tie ở trên.
- **An toàn float64 của Redis ZSET**: max = `(2^20-1)<<32 + 2^32-1 ≈ 4.5e15 <
  2^53` → int64 ↔ float64 không mất chính xác. (Draft v1 lo "mất chính xác
  đuôi" — hết lo với encoding này.)
- **Đủ dải rank**: bucket 20 bit = rank score 0..1048.575 với 3 số lẻ. Local
  score thực tế < 200 (log1p(10^6)*10 ≈ 138 + 20 + 10); blend CF cộng thêm vẫn
  dư. Rank âm clamp về 0 (công thức local không âm).
- **Cursor không đổi kiểu**: `TimelinePosition.Score` vẫn `int64`,
  `FeedCursor.TimelineScore` (`tl_score`) và validation `>= 0` giữ nguyên.
- Độ phân giải 0.001 rank là chủ đích: hai post lệch nhau < 0.001 điểm coi như
  ngang nhau, để createdAt quyết định — hành vi mong muốn.

`TimelineScoreFromTime` bị xoá trong P1 (thay bằng `PackTimelineScore`).

### 4.2 Ranker abstraction — thêm nguồn codohue

- Hiện `feed.Ranker` chỉ có `LocalRanker`.
- Thêm một **re-ranker tầng cao** dùng ở background (KHÔNG phải hot path):
  ```
  type TimelineRanker interface {
      // Trả rank score (float, CHƯA pack) theo post ID;
      // item nào codohue không biết → dùng local.
      RankForUser(ctx, userID, posts, followingSet, now) (map[postID]float64, error)
  }
  ```
  Impl `CodohueTimelineRanker`:
  1. Tính local score cho tất cả (floor).
  2. Gọi `codohue.Client.Rank(subjectID=userID, candidates=[]postID)` (đã có
     sẵn ở `pkg/codohue/client.go`).
  3. Với item có `RankedItem.Score > 0` →
     **`final = local + FEED_RANK_CF_WEIGHT × cf`** (quyết định blend — xem
     §11/Q2). Item CF = 0 hoặc call lỗi/timeout → giữ nguyên local (degrade êm).
  4. Caller pack kết quả bằng `PackTimelineScore(final, post.CreatedAt)` trước
     khi ghi store.
- Lưu ý: thang điểm CF của codohue chưa khảo sát (có thể là similarity 0..1,
  nhỏ so với local ~30). `CF_WEIGHT` phải tunable và có metric để quan sát rồi
  chỉnh ở P4 — không đoán trước.
- `feed.Recommender.GetRecommendations` (tầng candidate hiện tại) **vẫn giữ** cho
  discovery/khám phá bài lạ; Rankings chỉ lo *thứ tự* của candidate đã có.

### 4.3 Refresher (background) — `PreparedTimelineRefresher.refreshOne`

- Hiện: lấy following posts → entries score=timestamp → `AddPostsBatch`.
- Đổi thành:
  1. Lấy candidate như cũ (following; mở rộng theo Q4 nếu chốt).
  2. Gọi `TimelineRanker.RankForUser` → map điểm (local-only khi
     `FEED_RANK_SOURCE=local`).
  3. `entries[i].Score = PackTimelineScore(score[postID], post.CreatedAt)`.
  4. **`SetPostsBatch`** (ghi đè score — xem §4.7). KHÔNG dùng `AddPostsBatch`
     như draft v1: method đó `ZADD NX` nên không bao giờ update score của
     member đã tồn tại → re-rank sẽ ghi vào hư không.
- Đây chính là **background re-rank job (B)**. `RefreshOnMiss` hiện có tái dùng
  cơ chế này; thêm trigger định kỳ (xem §4.6).

### 4.4 Fan-out on write (A) — `fanout.go` / dispatcher

- Hiện: `EmitPostCreated` set `Event.Score = TimelineScoreFromTime(createdAt)`
  (`dispatcher.go`), fanout ghi nguyên score đó qua `AddPost` (NX).
- Đổi: `Event.Score = PackTimelineScore(RecencyScale + RelationshipBonus,
  createdAt)`. Giải thích:
  - Điểm write-time của post mới là hằng số `RecencyScale + RelationshipBonus`
    (= 30 với config mặc định — xem phân tích ở §4.1); ta dùng thẳng hằng số
    này cho minh bạch thay vì giả vờ "chấm điểm". Thành phần createdAt trong
    encoding lo thứ tự mới-nhất-trước giữa các post fan-out.
  - Dispatcher cần được inject `ScorerConfig` lúc wiring (`internal/app`) để
    lấy 2 hằng số này.
- Hành vi tương đối so với post đã re-rank là **chủ đích**: post cũ 2h không
  like ≈ 13.85 điểm < post mới 30 điểm (mới nổi lên trên ✓); post cũ nhiều
  like (vd 50 like ≈ 53 điểm) > post mới (bài hot đứng trên bài vừa đăng —
  đúng tính chất ranked feed).
- Không gọi codohue ở fan-out (giữ đường ghi nhanh, số lượng lớn).
- Fan-out **giữ `AddPost` (NX)**: nếu post đã có trong timeline với điểm
  re-rank (event đến muộn sau một lần refresh), NX bảo vệ không cho điểm
  write-time ghi đè điểm CF tốt hơn.

### 4.5 Hot path — `getFeedFromTimeline`

- **Bỏ** bước `rankCandidates` realtime.
- Đọc ZSET (đã theo rank), hydrate `GetPostsByIDs`, **sort lại kết quả hydrate
  theo đúng thứ tự entries của ZSET** — `GetPostsByIDs` (`WHERE id = ANY`)
  không bảo toàn thứ tự. (Phát hiện khi implement: timeline path trước giờ
  KHÔNG hề sort — `rankCandidates` chỉ tính điểm hiển thị, `sortFeedItems` chỉ
  chạy ở mixed path — nên thứ tự trang âm thầm theo thứ tự DB trả về, một bug
  tiềm ẩn mà bước sort tường minh này sửa luôn.)
- Enrich `isLiked`/`isFollowing`, filter eligibility như cũ, cắt trang, trả.
- Cursor: `TimelinePosition{Score, PostID}` vẫn hoạt động vì phân trang theo
  score ZSET. Quyết định cursor "mềm" — xem §11/Q1.

### 4.6 Trigger re-rank job (B)

Các phương án (chọn 1 hoặc kết hợp, tham số hoá):
- **Lazy on read miss** — đã có (`RefreshOnMiss`); nâng cấp để chạy full re-rank.
- **Theo behavior event** — khi user LIKE/COMMENT/VIEW đủ nhiều, enqueue re-rank
  cho chính user đó (subject vừa đổi → CF đáng tính lại).
- **Định kỳ cho active users** — cron/worker quét user hoạt động gần đây, re-rank
  giãn cách (vd mỗi 15–30 phút). Tránh re-rank user ngủ đông.

### 4.7 TimelineStore — thêm `SetPostsBatch` (ghi đè score)

Hiện trạng: `AddPostsBatch` trong `RedisTimelineStore` dùng
`ZAddArgs{NX: true}` — chỉ thêm member mới, **không update score member đã
có**. Semantics này đúng cho fan-out (không clobber điểm re-rank — §4.4) nhưng
sai hoàn toàn cho re-rank job. Bổ sung:

```go
type TimelineStore interface {
    AddPost(ctx, userID, entry) error            // NX — fan-out
    SetPostsBatch(ctx, userID, entries) error    // MỚI: ZADD thường (upsert),
                                                 // ghi đè score — refresher/re-rank
    ReadPage(...)
    Trim(...)
    RemovePostBestEffort(...)
}
```

(Chốt ở review U1: `AddPostsBatch` bị **bỏ khỏi interface** — sau khi refresher chuyển
sang `SetPostsBatch` nó không còn caller production nào; Redis impl dùng helper nội bộ
`writeBatch(nx bool)` chung cho cả hai đường ghi.)

- `SetPostsBatch` = pipeline `ZADD` (không NX) + `ZRemRangeByRank` trim +
  `Expire`, y hệt `AddPostsBatch` chỉ khác flag.
- **Không** làm kiểu `DEL` + rebuild key: race với fan-out chạy song song (post
  mới ghi vào giữa DEL và ZADD sẽ mất). Entry cũ thừa (unfollow…) đã có
  follow/unfollow event xử lý remove + `isEligibleTimelinePost` lọc ở read —
  đủ.
- Cần implement ở cả `RedisTimelineStore` lẫn `NopTimelineStore` (+ tests cho
  case "NX không ghi đè, Set ghi đè").

## 5. Migration & rollout (đổi ngữ nghĩa score)

Bản draft v1 thiếu hẳn phần này. Vấn đề: sau deploy P1, ZSET cũ chứa score
timestamp-micro (~1.7e15), entry mới là packed score (max ≈ 4.5e15) — **hai dải
chồng lấn nhau**, không phân biệt được bằng giá trị, và vì `AddPostsBatch` NX
nên refresh cũng không sửa được entry cũ. Giải pháp:

1. **Version bump key prefix**: `feed:tl` → `feed:tl:v2`
   (`redis_timeline_store.go`). Timeline v2 build lại từ đầu qua lazy
   `RefreshOnMiss` sẵn có — user đọc feed lần đầu sau deploy sẽ trigger refresh
   (một lần, chi phí như timeline miss bình thường). Key cũ tự chết theo TTL
   (mặc định 7 ngày), không cần dọn tay.
2. **Cursor cũ đang bay (in-flight)**: `tl_score` cũ ~1.7e15 đọc trên key v2 sẽ
   hành xử như "đọc từ đầu" (mọi packed score đều < giá trị đó trừ khi rank >
   ~413k — không xảy ra) → user đang cuộn dở tại thời điểm deploy thấy lại
   trang đầu **một lần**. Chấp nhận: sự kiện one-off trong deploy window, khớp
   quyết định feed "mềm" (§11/Q1). Không thêm version field vào cursor.
3. **Không cần data migration** — không có dữ liệu Postgres nào đổi; toàn bộ là
   Redis derived data, rebuild được từ nguồn.
4. Rollback P1 = revert code: key v1 (nếu còn TTL) hoặc rebuild v1 qua chính
   lazy refresh cũ. Không một chiều.

## 6. Data flow tổng hợp

**Ghi (post mới):** `CreatePost` → fan-out (A) → ZSET follower có post mới,
score = packed(hằng số write-time, createdAt) → hiện ngay, mới-nhất-trước.

**Nền (định kỳ/triggered):** re-rank job (B) → codohue Rankings + local fallback
→ `SetPostsBatch` ghi đè score cho user active.

**Đọc (request feed):** hot path → `ZREVRANGE` → hydrate → sort theo ZSET →
enrich → trả. Không rank, không codohue.

## 7. Config mới (đề xuất)

| Env | Ý nghĩa | Default |
|---|---|---|
| `FEED_RANK_SOURCE` | `local` \| `codohue` (bật re-rank CF ở background) | `local` |
| `FEED_RANK_CF_WEIGHT` | trọng số W trong `final = local + W×cf` | `1.0` |
| `FEED_RERANK_INTERVAL` | chu kỳ re-rank active user | `30m` |
| `FEED_RERANK_ON_EVENT` | re-rank ngay khi subject đổi (event) | `false` |
| `CODOHUE_RANK_TIMEOUT` | timeout call Rankings trong job | `3s` |

Tất cả có default giữ nguyên hành vi hiện tại (local), bật dần.

Lưu ý plumbing: `codohue.Client.Rank` hiện **hardcode timeout 5s** bên trong
(`pkg/codohue/client.go`) — muốn `CODOHUE_RANK_TIMEOUT` có tác dụng phải thêm
tham số/option cho client thay vì chỉ set ở caller.

## 8. Trade-offs & rủi ro

- **Cursor "mềm"** (đã chốt — §11/Q1): score đổi khi re-rank → post có thể nhảy
  chỗ giữa 2 lần load (thấy lại / bỏ lỡ item quanh biên trang). Chấp nhận: chu
  kỳ re-rank (15–30m) dài hơn nhiều so với một phiên cuộn, tần suất va chạm
  thấp.
- **Staleness**: hot path đọc điểm đã tính từ trước → feed hơi "cũ" giữa 2 lần
  re-rank. Đánh đổi lấy latency ổn định. Chu kỳ re-rank điều chỉnh được.
- **Cold start / thiếu data**: nếu chưa có VIEW/interaction (tình trạng hiện
  tại), codohue Rankings trả score 0 → fallback local hết → **kết quả y hệt local
  bây giờ**, nhưng đã đúng kiến trúc. Tức làm trước khi có data thì không thấy
  khác biệt (xem §10).
- **Chi phí background**: re-rank N user × M candidate = nhiều call codohue. Cần
  giới hạn (chỉ active user, batch, giãn cách).
- ~~Score scaling / precision~~ — đã giải quyết bằng packed encoding (§4.1):
  max ≈ 4.5e15 < 2^53, int64 ↔ float64 exact.
- ~~Tie hàng loạt ở fan-out~~ — đã giải quyết bằng thành phần createdAt trong
  encoding (§4.1). Hệ quả phụ tốt: khối tie lớn từng đe doạ logic đọc
  `Count: limit*2` trong `ReadPage` cũng không còn.

## 9. Thứ tự triển khai (phased, mỗi phase tự đứng được)

1. **P1 — Đổi trục điểm, giữ local**:
   - `PackTimelineScore` + xoá `TimelineScoreFromTime` (§4.1);
   - `SetPostsBatch` cho cả 2 store + tests (§4.7);
   - refresher rank local + pack + `SetPostsBatch` (§4.3);
   - `EmitPostCreated` ghi packed write-time score, inject `ScorerConfig` vào
     dispatcher (§4.4);
   - hot path bỏ `rankCandidates`, sort theo thứ tự ZSET (§4.5);
   - key prefix `feed:tl:v2` (§5).
   Kết quả: kiến trúc materialized đúng, vẫn 100% local, không cần codohue.
   *An toàn, đo được ngay (hot path nhẹ đi), rollback bằng revert.*
2. **P2 — Cắm codohue Rankings vào background** (`CodohueTimelineRanker` +
   `FEED_RANK_SOURCE=codohue`), fallback local, plumbing
   `CODOHUE_RANK_TIMEOUT` (§7). Chưa cần trigger phức tạp: dùng lazy-refresh
   sẵn có.
3. **P3 — Trigger re-rank** theo event + định kỳ cho active user (chốt Q3 trước
   khi làm).
4. **P4 — Tinh chỉnh blend** (`CF_WEIGHT`), khảo sát thang điểm CF thực tế,
   quan sát metrics (đã có `feed/metrics.go`: timeline hit/miss, fanout...).
   Thêm metric re-rank (số call, tỉ lệ CF≠0, latency).

**Tiền đề bắt buộc trước P2 có ý nghĩa**: có behavior data (VIEW events + tương
tác). Không có thì P2/P3 chạy nhưng vô hình (fallback local hết).

## 10. Phụ thuộc: dữ liệu hành vi

CF của codohue vô nghĩa nếu không có interaction graph. Hiện darkvoid chỉ phát
`LIKE/COMMENT/SKIP`, **thiếu `VIEW`** (signal lớn nhất) → 0 subject. Vì vậy:

- **P1 làm được ngay** (thuần kiến trúc, không cần data).
- **P2+ chỉ nên làm sau khi có VIEW tracking và/hoặc bot tương tác** để codohue
  Rankings trả điểm khác 0.

→ Đề xuất thứ tự thực tế: **P1 (kiến trúc)** song song với **VIEW tracking / bot
interaction (data)**, rồi mới **P2 (nối Rankings)**.

## 11. Quyết định review

**Q1 — Cursor: chấp nhận feed "mềm" hay snapshot score vào cursor? →
CHỐT: feed "mềm".** Lý do: snapshot score trong cursor không thực sự ổn định
được gì — `ReadPage` query ZSET theo score *hiện tại*, muốn ổn định thật phải
snapshot cả timeline per-session (tốn Redis, thêm cơ chế dọn), không đáng cho
feed xã hội. Filter `(score, postID)` sẵn có đã chống lặp vô hạn trong một
trang; xê dịch giữa các trang chấp nhận được vì chu kỳ re-rank dài hơn phiên
cuộn.

**Q2 — Blend: CF thay hẳn local hay cộng trọng số? → CHỐT: cộng trọng số
`final = local + W×cf` (W = `FEED_RANK_CF_WEIGHT`, default 1.0).** Lý do:
local mang thành phần recency — nếu CF thay hẳn thì post mới (CF chưa có
signal) bị chôn, mất tính realtime của fan-out; cộng trọng số giữ đúng nguyên
tắc "local là floor, codohue là lớp phủ" (§3). Thang điểm CF chưa khảo sát nên
W phải tunable + có metric (P4).

**Q3 — Trigger re-rank: lazy-only trước hay làm luôn định kỳ + event? — CÒN
MỞ.** Không chặn P1/P2 (P2 dùng lazy-refresh sẵn có); chốt trước khi làm P3.

**Q4 — Có kéo trending/recommendations vào candidate của timeline không? —
CÒN MỞ.** Không chặn P1 (candidate giữ nguyên following); ảnh hưởng §4.3 bước
1 nếu chốt "có".
