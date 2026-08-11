# Project Audit — 2026-08-11

## Kết luận

Dự án có cấu trúc modular monolith tương đối rõ ràng và các kiểm tra tự động hiện tại đều đạt. Tuy nhiên, dự án chưa nên public hoặc triển khai production trước khi xử lý các vấn đề P0 về phân quyền nội dung, upload, storage và error handling.

Các vùng rủi ro chính:

- Quyền truy cập post/comment chưa bảo vệ visibility.
- Upload avatar/cover có thể tạo stored XSS.
- Cấu hình storage không được hỗ trợ có thể làm mất file âm thầm.
- Materialized feed có thể mất event, lặp hoặc bỏ sót dữ liệu.
- Notification/SSE có lỗi concurrency và cache consistency.
- Refresh-token lifecycle chưa atomic và token được lưu dạng raw.
- Cấu hình production chưa phù hợp với multi-instance và quy trình backup an toàn.

## Trạng thái khắc phục

Các finding P0 đã được sửa trong worktree ngày 2026-08-11:

| ID | Trạng thái | Thay đổi chính |
|---|---|---|
| P0-01 | Resolved | Policy visibility dùng chung được áp dụng cho post, comment, reply, post-like và comment-like; route đọc dùng optional auth; user-post query chỉ nhận tập visibility đã được authorize |
| P0-02 | Resolved | Avatar/cover được decode, giới hạn kích thước/pixel và re-encode thành JPEG/PNG; media tự sniff bytes; local uploads có `nosniff` và CSP sandbox |
| P0-03 | Resolved | Storage factory và config validation fail startup với `s3`, provider rỗng hoặc không hỗ trợ; cấu hình S3 chưa triển khai đã được gỡ khỏi env/docs |
| P0-04 | Resolved | `WithDetail` copy-on-write; panic chỉ được log nội bộ với stack và client luôn nhận generic `INTERNAL_ERROR` |

Các finding P1 đã được sửa trong worktree ngày 2026-08-11:

| ID | Trạng thái | Thay đổi chính |
|---|---|---|
| P1-01 | Resolved | Post và follow mutation ghi feed event vào transactional PostgreSQL outbox; consumer có lease, retry/backoff, dead-letter, metric và timeline upsert idempotent; queue đầy fallback đồng bộ và emitter trả lỗi đúng |
| P1-02 | Resolved | Follower reader nhận limit từ runtime setting (`cap+1`) thay vì hard-code 5.000; bổ sung metric followers/attempted/succeeded/failed/capped |
| P1-03 | Resolved | Timeline đọc dư một item và dùng `HasMore`; continuation rỗng/stale kết thúc đúng cursor family, không khởi động lại mixed feed |
| P1-04 | Resolved | Redis timeline đọc lặp theo chunk đến khi đủ trang hoặc hết ZSET; có integration test tie-score block 130 phần tử |
| P1-05 | Resolved | Refresh dùng atomic snapshot replacement nhưng giữ fanout đồng thời; delete/visibility event chủ động loại stale entry |
| P1-06 | Resolved | Private post được materialize chỉ vào timeline tác giả và batch hydration cho phép service eligibility trả post đó cho owner |
| P1-07 | Resolved | Cursor chỉ advance recommendation đã emit hoặc invalid; offset đã emit lệch thứ tự được giữ trong cursor để không skip item bị outrank |
| P1-08 | Resolved | Broker đồng bộ deliver/cleanup/shutdown dưới RW lock, cleanup/shutdown idempotent; có stress/race test |
| P1-09 | Resolved | Bỏ `EXISTS` + `INCR`; mọi create/upsert/delete/read mutation invalidate unread cache để rebuild từ DB |
| P1-10 | Resolved | Refresh token chỉ lưu SHA-256 hash; consume-and-rotate nằm trong một transaction và token cũ chỉ được consume một lần |
| P1-11 | Resolved | Account-detail endpoint chỉ trả `UserResponse` khi key UUID/username resolve về chính authenticated user; thêm authorization tests |
| P1-12 | Resolved | Auth middleware chỉ nhận Bearer token qua `Authorization`; query-token và Swagger parameter đã bị loại bỏ |
| P1-13 | Resolved | Post-like và comment-like toggle áp dụng self-like guard cùng transaction advisory lock theo cặp tài nguyên, trả trạng thái committed trước khi phát side effect |

Tác động triển khai của P1:

- Phải chạy migration user `000014` và `000015` trước khi chạy binary mới. `000014` chuyển token hiện hữu sang hash một chiều; down migration chỉ khôi phục cấu trúc bằng giá trị hash, không thể khôi phục raw token.
- Client SSE không còn được gửi JWT trong query string. Client phải dùng request streaming có `Authorization: Bearer ...`; native `EventSource` cần chuyển sang cookie/ticket ngắn hạn trong một thay đổi riêng nếu được sử dụng.
- `GET /users/{userKey}` nay là account-detail self-only; dữ liệu public của user khác tiếp tục đi qua profile endpoint.

Xác minh thay đổi P1: `make generate`, `make build`, `make test`, `go test -race ./...`, `go vet ./...` và `golangci-lint run ./...` đều đạt. Redis integration test cho atomic replacement và tie-score block đã được thêm nhưng bị skip trong lần chạy này do không cấu hình `REDIS_TEST_ADDR`.

Các thay đổi đã qua `make build`, `make test`, `go test -race ./...`, `go vet ./...` và `golangci-lint run ./...`.

## Phạm vi và phương pháp kiểm tra

Audit bao gồm:

- Cấu trúc package và ranh giới `handler/service/repository/entity/dto`.
- Luồng authentication, authorization và error response.
- Post, comment, like, follow và user profile.
- Materialized ranked feed, Redis timeline và cursor pagination.
- Notification cache, SSE broker và Redis Pub/Sub.
- Upload/storage và static file serving.
- JWT, refresh token và request parsing.
- Docker/Compose, migration và backup.
- Test, race detector, vet và lint.

Kết quả xác minh:

| Kiểm tra | Kết quả | Ghi chú |
|---|---:|---|
| `make build` | Đạt | Build thành công |
| `make test` | Đạt | Chạy ngoài sandbox để cho phép localhost listener |
| `go test -race ./...` | Đạt | Không phát hiện race trong các package có test |
| `go vet ./...` | Đạt | Không có lỗi |
| `golangci-lint` | Đạt | 21 linters, 0 issue khi dùng writable cache |
| Package documentation | Đạt | Không phát hiện package thiếu `docs.go` |

> Lưu ý: kết quả test/race xanh không bao phủ các package không có test, đặc biệt là notification broker/cache/service, search, storage, `pkg/errors`, JWT và logger. Các integration test của Redis timeline cũng bị skip nếu không có `REDIS_TEST_ADDR`.

## P0 — phải sửa trước khi production

### P0-01: Post private/followers có thể bị đọc công khai — Resolved

**Mức độ:** Critical
**Loại:** Broken access control / data exposure

> Đã sửa bằng authorization policy fail-closed tại service layer. Phần bằng chứng dưới đây ghi nhận trạng thái trước khi sửa.

#### Bằng chứng

- Các route đọc post, danh sách post, comment và reply không dùng auth middleware: [`internal/app/post_routes.go:12`](../internal/app/post_routes.go#L12).
- `GetPost` nhận viewer nhưng không kiểm tra visibility, owner hoặc quan hệ follow: [`internal/feature/post/service/post_service.go:200`](../internal/feature/post/service/post_service.go#L200).
- SQL lấy post theo ID chỉ kiểm tra `deleted_at`: [`internal/feature/post/sql/post_queries.sql:8`](../internal/feature/post/sql/post_queries.sql#L8).
- Endpoint danh sách chấp nhận trực tiếp `?visibility=private`: [`internal/feature/post/handler/post_handler.go:248`](../internal/feature/post/handler/post_handler.go#L248).
- Khi visibility rỗng, SQL trả tất cả visibility: [`internal/feature/post/sql/post_queries.sql:84`](../internal/feature/post/sql/post_queries.sql#L84).

#### Ảnh hưởng

- Biết UUID là có thể đọc post `private` hoặc `followers`.
- Comment và reply của post không công khai cũng có thể bị đọc.
- User đã đăng nhập có thể like/comment nội dung mà họ không có quyền xem vì mutation service chỉ kiểm tra post tồn tại.

#### Khuyến nghị

- Tạo policy dùng chung như `CanViewPost(viewerID, post)`.
- Áp dụng policy cho get/list/comment/reply/like/comment-like.
- Dùng optional auth trên các route đọc để có viewer identity.
- Với post không được phép xem, ưu tiên trả 404 để tránh xác nhận tài nguyên tồn tại.
- Viết test matrix cho `public`, `followers`, `private`, owner, follower, non-follower và anonymous.

---

### P0-02: Stored XSS qua avatar và cover — Resolved

**Mức độ:** Critical
**Loại:** Unrestricted file upload / stored XSS

> Đã sửa bằng content validation, decode/re-encode và response hardening cho user uploads. Phần bằng chứng dưới đây ghi nhận trạng thái trước khi sửa.

#### Bằng chứng

- Handler chỉ giới hạn kích thước rồi chuyển `Content-Type` và extension do client cung cấp: [`internal/feature/user/handler/profile_handler.go:166`](../internal/feature/user/handler/profile_handler.go#L166), [`internal/feature/user/handler/profile_handler.go:214`](../internal/feature/user/handler/profile_handler.go#L214).
- Service giữ nguyên extension trong storage key: [`internal/feature/user/service/user_service.go:336`](../internal/feature/user/service/user_service.go#L336), [`internal/feature/user/service/user_service.go:372`](../internal/feature/user/service/user_service.go#L372).
- Upload local được phục vụ công khai cùng origin qua `http.FileServer`: [`internal/app/app.go:383`](../internal/app/app.go#L383).
- Không có CSP hoặc `X-Content-Type-Options: nosniff` trong middleware toàn cục.

#### Ảnh hưởng

Attacker có thể upload file `.html` hoặc nội dung chủ động rồi mở nó dưới origin của API. Điều này có thể cho phép thực thi JavaScript cùng origin, gọi API bằng cookie hiện tại hoặc lợi dụng refresh flow.

#### Khuyến nghị

- Luôn sniff magic bytes, không tin multipart `Content-Type`.
- Chỉ cho phép JPEG, PNG hoặc WebP thực sự.
- Tự sinh extension từ MIME đã xác minh.
- Decode và re-encode ảnh để loại payload/polyglot.
- Phục vụ user-upload từ CDN/domain riêng không mang cookie.
- Thêm `X-Content-Type-Options: nosniff` và CSP phù hợp.
- Áp dụng cùng nguyên tắc cho media upload; hiện endpoint media chỉ sniff khi client không gửi `Content-Type`: [`internal/feature/storage/handler/media_handler.go:59`](../internal/feature/storage/handler/media_handler.go#L59).

---

### P0-03: Provider storage không hỗ trợ làm mất file âm thầm — Resolved

**Mức độ:** Critical
**Loại:** Silent data loss / deployment misconfiguration

> Đã sửa theo hướng fail-closed ở cả config validation và storage factory. Phần bằng chứng dưới đây ghi nhận trạng thái trước khi sửa.

#### Bằng chứng

- Config mô tả provider `local` hoặc `s3`: [`pkg/config/config.go:136`](../pkg/config/config.go#L136).
- `storage.New` chỉ hỗ trợ `local`; mọi giá trị khác được chuyển sang `nopStorage`: [`pkg/storage/storage.go:17`](../pkg/storage/storage.go#L17).
- `nopStorage.Put` và `Delete` luôn trả `nil`: [`pkg/storage/storage.go:45`](../pkg/storage/storage.go#L45).
- Application vẫn log storage đã khởi tạo thành công: [`internal/app/infrastructure_setup.go:28`](../internal/app/infrastructure_setup.go#L28).

#### Ảnh hưởng

Nếu đặt `STORAGE_PROVIDER=s3`, upload trả thành công và DB lưu key nhưng không có file nào được ghi.

#### Khuyến nghị

- Provider không nhận diện phải làm ứng dụng fail boot.
- Không dùng `nopStorage` ngoài test hoặc development được bật rõ ràng.
- Triển khai S3-compatible storage thực sự trước khi công bố hỗ trợ.
- Thêm startup validation và integration test put/get/delete cho từng provider.

---

### P0-04: Mutable global errors gây race và lộ nội dung panic — Resolved

**Mức độ:** Critical
**Loại:** Information disclosure / shared mutable state / concurrency

> Đã sửa bằng copy-on-write error details và generic panic response. Phần bằng chứng dưới đây ghi nhận trạng thái trước khi sửa.

#### Bằng chứng

- `WithDetail` mutate trực tiếp `AppError.Details`: [`pkg/errors/errors.go:30`](../pkg/errors/errors.go#L30).
- Các lỗi thông dụng là singleton toàn cục: [`pkg/errors/codes.go:8`](../pkg/errors/codes.go#L8).
- Panic handler ghi raw panic vào `ErrInternal` rồi trả cho client: [`pkg/errors/response.go:53`](../pkg/errors/response.go#L53).
- Password validation mutate `user.ErrWeakPassword`: [`internal/feature/user/service/validation.go:76`](../internal/feature/user/service/validation.go#L76).

#### Ảnh hưởng

- Raw panic/internal value bị trả về client.
- Detail của request trước có thể tồn tại trong response sau.
- Concurrent requests có thể data race hoặc concurrent map access.
- Chi Recoverer phía ngoài không còn cơ hội log stack nếu inner handler đã recover.

#### Khuyến nghị

- Biến `AppError` thành immutable value hoặc để `WithDetail` clone struct/map.
- Không thêm panic value vào response.
- Log panic và stack trace nội bộ với request ID.
- Chỉ giữ một panic recovery middleware có trách nhiệm rõ ràng.
- Thêm concurrent tests cho error construction và panic recovery.

## P1 — lỗi lớn về tính đúng, bảo mật và concurrency

### P1-01: Feed event có thể mất vĩnh viễn — Resolved

**Mức độ:** High
**Loại:** Reliability / eventual consistency

#### Bằng chứng

- Dispatcher dùng non-blocking in-process channel và trả `false` khi queue đầy/đóng/tắt: [`internal/feature/feed/dispatcher.go:106`](../internal/feature/feed/dispatcher.go#L106).
- Các emitter bỏ qua kết quả `Dispatch` rồi luôn trả `nil`: [`internal/feature/feed/dispatcher.go:161`](../internal/feature/feed/dispatcher.go#L161).
- Post service chỉ log khi emitter trả error, nhưng emitter hiện không bao giờ trả error cho enqueue failure: [`internal/feature/post/service/post_service.go:191`](../internal/feature/post/service/post_service.go#L191).

#### Ảnh hưởng

Event mất khi queue đầy, process crash/restart, worker timeout hoặc Redis lỗi. Timeline đã tồn tại không phải cache miss nên refresh-on-miss không sửa được; post mới có thể không xuất hiện cho một số user.

#### Khuyến nghị

- Dùng transactional outbox trong PostgreSQL.
- Durable consumer với retry, dead-letter và idempotent timeline upsert.
- Nếu chưa có outbox, ít nhất phải đánh dấu timeline dirty và phát metric/alert khi enqueue thất bại.
- Không trả success âm thầm khi persistence side effect bắt buộc thất bại.

---

### P1-02: Fanout follower cap không đúng với runtime settings — Resolved

**Mức độ:** High
**Loại:** Feed correctness / configuration drift

#### Bằng chứng

- Fanout áp dụng cap từ runtime settings: [`internal/feature/feed/fanout.go:87`](../internal/feature/feed/fanout.go#L87).
- Nhưng `GetFollowerIDs` đã hard-code giới hạn 5.000 trước đó: [`internal/feature/user/service/follow_service.go:178`](../internal/feature/user/service/follow_service.go#L178).

#### Ảnh hưởng

Mọi cấu hình cap trên 5.000 không có tác dụng. Follower nằm ngoài 5.000 record đầu sẽ không nhận fanout.

#### Khuyến nghị

- Đưa limit vào interface/call site hoặc phân trang toàn bộ follower list tới configured cap.
- Ghi metric cho `total followers`, `attempted`, `succeeded`, `failed`, `capped`.

---

### P1-03: Timeline cursor có thể chuyển sang mixed feed và lặp dữ liệu — Resolved

**Mức độ:** High
**Loại:** Pagination correctness

#### Bằng chứng

- Timeline chỉ được coi là hit nếu trả ít nhất một item; trang rỗng sẽ rơi xuống mixed path: [`internal/feature/feed/service/feed_service.go:114`](../internal/feature/feed/service/feed_service.go#L114).
- Next cursor được sinh khi có đúng `pageSize` item dù chưa biết còn dữ liệu hay không: [`internal/feature/feed/service/feed_service.go:307`](../internal/feature/feed/service/feed_service.go#L307).

#### Ảnh hưởng

Nếu trang trước đúng bằng số item cuối cùng, client vẫn nhận cursor. Request tiếp theo đọc timeline rỗng rồi chuyển sang mixed path nhưng timeline cursor không có following/trending position, khiến feed có thể bắt đầu lại và trả duplicate.

#### Khuyến nghị

- Fetch `pageSize+1` để xác định continuation.
- Biểu diễn trạng thái `timeline exhausted` rõ ràng trong cursor.
- Không chuyển cursor family ngầm định.
- Thêm characterization tests cho exact-end, stale-only page và handoff sang discover.

---

### P1-04: Redis timeline pagination bỏ sót tie-score block lớn — Resolved

**Mức độ:** High
**Loại:** Pagination correctness / Redis data access

#### Bằng chứng

- Redis query lấy tối đa `limit*2` entry theo score: [`internal/feature/feed/cache/redis_timeline_store.go:77`](../internal/feature/feed/cache/redis_timeline_store.go#L77).
- Tie-break theo UUID được lọc sau ở Go: [`internal/feature/feed/cache/redis_timeline_store.go:103`](../internal/feature/feed/cache/redis_timeline_store.go#L103).

#### Ảnh hưởng

Nếu có hơn `2*limit` member cùng score đứng trước cursor, toàn bộ chunk có thể bị lọc và trang trả rỗng mặc dù vẫn còn member hợp lệ phía sau.

#### Khuyến nghị

- Cài tuple continuation `(score, member)` đầy đủ bằng Redis/Lua.
- Hoặc fetch lặp theo chunk cho tới khi đủ limit hoặc thực sự hết.
- Thêm Redis integration test với tie block lớn hơn fetch window.

---

### P1-05: Timeline refresh không xóa entry stale — Resolved

**Mức độ:** High
**Loại:** Cache consistency / feed correctness

#### Bằng chứng

- Refresher chỉ upsert bằng `SetPostsBatch`: [`internal/feature/feed/refresher.go:72`](../internal/feature/feed/refresher.go#L72).
- Unfollowed authors được giữ lại có chủ ý: [`internal/feature/feed/fanout.go:61`](../internal/feature/feed/fanout.go#L61).
- `EventPostDeleted`, `EventVisibilityChanged` và `RemovePostBestEffort` được định nghĩa nhưng không có production path sử dụng: [`internal/feature/feed/dispatcher.go:15`](../internal/feature/feed/dispatcher.go#L15), [`internal/feature/feed/timeline.go:40`](../internal/feature/feed/timeline.go#L40).

#### Ảnh hưởng

Entry từ author đã unfollow, post đã xóa hoặc visibility đã đổi tiếp tục chiếm timeline capacity và read window. Khi stale entry có rank cao, trang có thể ngắn/rỗng và kích hoạt fallback sai.

#### Khuyến nghị

- Dùng atomic replace với generation/version để không làm mất fanout đồng thời.
- Emit và handle delete/visibility events.
- Có scheduled repair/reconciliation cho timeline.

---

### P1-06: Feed của tác giả không nhất quán với private post — Resolved

**Mức độ:** High
**Loại:** Business-rule inconsistency

#### Bằng chứng

- Fanout bỏ qua private post hoàn toàn: [`internal/feature/feed/fanout.go:79`](../internal/feature/feed/fanout.go#L79).
- Batch hydration chỉ trả `public` và `followers`: [`internal/feature/post/sql/post_queries.sql:93`](../internal/feature/post/sql/post_queries.sql#L93).
- Nhưng eligibility logic nói private post hợp lệ cho chính tác giả: [`internal/feature/feed/service/feed_service.go:342`](../internal/feature/feed/service/feed_service.go#L342).

#### Ảnh hưởng

Code mô tả rằng tác giả thấy private post của chính mình trong feed, nhưng storage và query khiến điều này không thể xảy ra.

#### Khuyến nghị

- Chốt product rule và đồng bộ fanout, hydration, eligibility và tests.

---

### P1-07: Recommendation cursor bỏ qua item chưa hiển thị — Resolved

**Mức độ:** High
**Loại:** Pagination correctness / ranking

#### Bằng chứng

- Recommendation offset tăng theo toàn bộ trang provider đã fetch: [`internal/feature/feed/service/feed_service.go:393`](../internal/feature/feed/service/feed_service.go#L393).
- Sau đó candidates mới được blend/rank với following và trending.
- Cursor tiếp theo giữ offset đã tăng: [`internal/feature/feed/service/feed_service.go:537`](../internal/feature/feed/service/feed_service.go#L537).

#### Ảnh hưởng

Recommendation đã fetch nhưng bị nguồn khác outrank và chưa trả cho client sẽ bị bỏ qua vĩnh viễn ở trang sau.

#### Khuyến nghị

- Chỉ advance dựa trên recommendation đã thực sự emit.
- Hoặc giữ buffer/server-side continuation token từ recommendation provider.

---

### P1-08: SSE broker có race giữa deliver, cleanup và shutdown — Resolved

**Mức độ:** High
**Loại:** Concurrency / availability

#### Bằng chứng

- `deliverLocal` lấy reference tới map dưới `RLock`, unlock rồi mới iterate/send: [`internal/feature/notification/broker/broker.go:110`](../internal/feature/notification/broker/broker.go#L110).
- Cleanup đồng thời delete khỏi map rồi close channel: [`internal/feature/notification/broker/broker.go:66`](../internal/feature/notification/broker/broker.go#L66).
- Shutdown đóng channel nhưng không loại client khỏi map: [`internal/feature/notification/broker/broker.go:79`](../internal/feature/notification/broker/broker.go#L79).

#### Ảnh hưởng

- Concurrent map iteration/write.
- Send vào channel đã đóng.
- Panic hoặc race trong production.

#### Khuyến nghị

- Snapshot client list an toàn dưới lock và có per-client closed state.
- Hoặc giữ read lock qua iteration với close sequencing rõ ràng.
- Làm cleanup idempotent sau shutdown.
- Viết stress/race tests cho publish + disconnect + shutdown.

---

### P1-09: Unread notification count bị drift — Resolved

**Mức độ:** High
**Loại:** Cache consistency

#### Bằng chứng

- Cache thực hiện `EXISTS` rồi `INCR` thành hai thao tác không atomic: [`internal/feature/notification/cache/redis_notification_cache.go:49`](../internal/feature/notification/cache/redis_notification_cache.go#L49).
- SQL create có thể upsert notification cũ và đặt `is_read = FALSE`: [`internal/feature/notification/sql/notification_queries.sql:1`](../internal/feature/notification/sql/notification_queries.sql#L1).
- Service luôn increment cache sau create/upsert: [`internal/feature/notification/service/notification_service.go:103`](../internal/feature/notification/service/notification_service.go#L103).
- Delete notification không invalidate hoặc decrement cache: [`internal/feature/notification/service/notification_service.go:94`](../internal/feature/notification/service/notification_service.go#L94).

#### Ảnh hưởng

Unread badge có thể lớn hơn hoặc khác DB cho tới khi TTL hết/rebuild.

#### Khuyến nghị

- Phương án đơn giản: invalidate unread cache sau mọi mutation.
- Phương án tối ưu: update dựa trên DB state/`RowsAffected` trong transaction và dùng Lua cho Redis atomicity.

---

### P1-10: Refresh-token rotation không atomic và token lưu dạng raw — Resolved

**Mức độ:** High
**Loại:** Session security

#### Bằng chứng

- Refresh token được lưu và lookup trực tiếp theo raw token: [`internal/feature/user/sql/refresh_token_queries.sql:1`](../internal/feature/user/sql/refresh_token_queries.sql#L1).
- Rotation chạy validate, generate access, revoke old và create new thành các bước riêng: [`internal/feature/user/service/auth_service.go:166`](../internal/feature/user/service/auth_service.go#L166).
- Revoke failure chỉ được log rồi flow vẫn tiếp tục: [`internal/feature/user/service/auth_service.go:191`](../internal/feature/user/service/auth_service.go#L191).

#### Ảnh hưởng

- DB read compromise cung cấp active refresh tokens dùng được ngay.
- Hai request đồng thời có thể dùng một token cũ để sinh hai token mới.
- Revoke thất bại vẫn trả session mới.

#### Khuyến nghị

- Lưu hash của opaque refresh token.
- Atomic consume-and-rotate trong một DB transaction.
- Revoke có điều kiện `WHERE is_revoked = false` và yêu cầu đúng một row affected.
- Phát hiện token reuse và revoke cả token family nếu cần bảo mật cao.

---

### P1-11: Endpoint user làm lộ email và trạng thái tài khoản — Resolved

**Mức độ:** High
**Loại:** Privacy / authorization

#### Bằng chứng

- Bất kỳ user đăng nhập nào cũng gọi được `GET /users/{userKey}/`: [`internal/app/user_routes.go:44`](../internal/app/user_routes.go#L44).
- Handler không kiểm tra self/admin: [`internal/feature/user/handler/user_handler.go:45`](../internal/feature/user/handler/user_handler.go#L45).
- `UserResponse` chứa `email` và `is_active`: [`internal/feature/user/dto/user_dto.go:25`](../internal/feature/user/dto/user_dto.go#L25).
- `ProfileResponse` đã chủ ý loại các trường nhạy cảm này: [`internal/feature/user/dto/user_dto.go:44`](../internal/feature/user/dto/user_dto.go#L44).

#### Khuyến nghị

- Giới hạn endpoint account detail cho self/admin.
- Dùng `ProfileResponse` cho người dùng khác.
- Thêm authorization test cho UUID và `?by=username`.

---

### P1-12: Bearer token được chấp nhận qua query string trên mọi route — Resolved

**Mức độ:** High
**Loại:** Credential exposure

#### Bằng chứng

- Middleware toàn cục fallback sang `?token=`: [`internal/app/middleware/auth.go:29`](../internal/app/middleware/auth.go#L29).

#### Ảnh hưởng

Token có thể xuất hiện trong browser history, referrer, reverse-proxy log, monitoring và analytics.

#### Khuyến nghị

- Chỉ cho phép bearer token trong `Authorization` header trên route thông thường.
- Với SSE/EventSource, dùng short-lived single-use stream ticket hoặc cookie scope phù hợp.

---

### P1-13: Toggle like không atomic về side effect và self-like rule không nhất quán — Resolved

**Mức độ:** High
**Loại:** Concurrency / business-rule inconsistency

#### Bằng chứng

- `Like()` cấm self-like: [`internal/feature/post/service/like_service.go:38`](../internal/feature/post/service/like_service.go#L38).
- `Toggle()` comment bỏ self-like guard: [`internal/feature/post/service/like_service.go:84`](../internal/feature/post/service/like_service.go#L84).
- Toggle dùng `IsLiked` rồi `Like/Unlike` theo kiểu check-then-act.

#### Ảnh hưởng

DB state có thể vẫn hợp lệ nhờ idempotent SQL, nhưng request đồng thời có thể trả cùng kết quả và phát notification/behavior event trùng.

#### Khuyến nghị

- Chốt rule self-like và áp dụng nhất quán.
- Dùng atomic SQL toggle/transaction trả về trạng thái mới.
- Chỉ phát side effect khi DB thực sự đổi trạng thái.

## P2 — kiến trúc, hardening và vận hành

### P2-01: Handler JSON không có body limit và strict decoding

**Mức độ:** Medium

Có nhiều handler gọi trực tiếp `json.NewDecoder(r.Body).Decode(...)`, ví dụ [`internal/feature/post/handler/post_handler.go:57`](../internal/feature/post/handler/post_handler.go#L57) và [`internal/feature/user/handler/auth_handler.go:96`](../internal/feature/user/handler/auth_handler.go#L96).

Khuyến nghị tạo helper chung:

- `http.MaxBytesReader`.
- `DisallowUnknownFields`.
- Chỉ cho phép một JSON document.
- Chuẩn hóa lỗi syntax/type/size.

---

### P2-02: JWT validation chấp nhận mọi HMAC algorithm

**Mức độ:** Medium

JWT được ký bằng HS256 nhưng validate chỉ yêu cầu token method thuộc nhóm HMAC: [`pkg/jwt/jwt.go:61`](../pkg/jwt/jwt.go#L61), [`pkg/jwt/jwt.go:83`](../pkg/jwt/jwt.go#L83).

Khuyến nghị:

- Yêu cầu chính xác `jwt.SigningMethodHS256`.
- Bắt buộc issuer/audience nếu chúng là một phần của trust boundary.
- Thêm tests cho wrong algorithm, issuer và audience.

---

### P2-03: Rate limiting phụ thuộc cấu hình reverse proxy

**Mức độ:** Medium, phụ thuộc deployment

`middleware.RealIP` chạy trước rate limiter: [`internal/app/server.go:46`](../internal/app/server.go#L46). Nếu app được expose trực tiếp hoặc proxy không sanitize forwarded headers, client có thể spoof IP và vượt rate limit.

Khuyến nghị:

- Chỉ tin forwarded headers từ trusted proxy.
- Không expose app container trực tiếp.
- Xác nhận reverse proxy luôn overwrite `X-Forwarded-For`/`X-Real-IP`.

---

### P2-04: Thiếu security headers

**Mức độ:** Medium

Không thấy middleware thiết lập CSP, `X-Content-Type-Options`, `Referrer-Policy` hoặc các header bảo vệ liên quan. Rủi ro tăng lên vì static user uploads được phục vụ cùng origin.

Khuyến nghị thêm middleware security headers và có cấu hình riêng cho API/static upload.

---

### P2-05: Access log không nhận được auth-enriched context

**Mức độ:** Medium

Comment nói access logger lấy logger mới nhất có `user_id`, nhưng middleware ngoài vẫn đọc context của request mà nó giữ tại [`pkg/logger/middleware.go:37`](../pkg/logger/middleware.go#L37). `r.WithContext` trong middleware phía trong không thay đổi request của middleware phía ngoài.

Ảnh hưởng: access log có thể thiếu `user_id`, làm giảm khả năng điều tra sự cố.

Khuyến nghị dùng response/request state chung hoặc context carrier có pointer-safe state được cập nhật xuyên middleware.

---

### P2-06: JSON response xử lý encode error sau khi đã gửi status

**Mức độ:** Low

`WriteJSON` gửi status trước, sau đó gọi `http.Error` nếu encode thất bại: [`internal/http/response.go:12`](../internal/http/response.go#L12). Khi header/body đã bắt đầu, status không thể đổi thành 500 và response có thể bị trộn JSON/plain text.

Khuyến nghị encode vào buffer trước khi commit header, hoặc chỉ log lỗi sau khi streaming đã bắt đầu.

---

### P2-07: Feed service quá lớn và gộp nhiều trách nhiệm

**Mức độ:** Medium

`internal/feature/feed/service/feed_service.go` dài khoảng 962 dòng, đang xử lý:

- Timeline rollout và cache.
- Timeline refresh/fallback.
- Mixed-source collection.
- Cursor state transitions.
- Ranking/deduplication.
- Enrichment và recommendation integration.

Khuyến nghị tách:

- Timeline reader/state machine.
- Following/trending/recommendation source adapters.
- Blend/ranking coordinator.
- Cursor transition module với property/table-driven tests.

Các file lớn khác cần xem xét dần: `internal/app/app.go`, `pkg/config/config.go`, `cmd/seed/main.go`.

---

### P2-08: Một số truy vấn/enrichment có nguy cơ N+1 hoặc pagination không ổn định

**Mức độ:** Medium

- Post enrichment kiểm tra follow theo từng author ở các post listing thông thường.
- Trending query chỉ `ORDER BY like_count DESC`, không có tie-break ID: [`internal/feature/post/sql/post_queries.sql:76`](../internal/feature/post/sql/post_queries.sql#L76).
- Follower/following offset pagination chỉ order theo timestamp có thể duplicate/skip khi concurrent insert.

Khuyến nghị batch follow lookup và dùng deterministic ordering `(score/time, id)` cho mọi cursor/order-sensitive query.

---

### P2-09: Local storage không phù hợp horizontal scaling

**Mức độ:** Medium/High tùy topology

Production compose mặc định dùng local named volume và `STORAGE_BASE_URL=http://localhost:8080/static`: [`docker-compose.prod.yml:282`](../docker-compose.prod.yml#L282).

Ảnh hưởng:

- Nhiều instance/host không nhìn thấy cùng file.
- Rolling replacement hoặc failover có thể làm media không nhất quán.
- URL localhost không phù hợp client bên ngoài nếu operator không override.

Khuyến nghị triển khai object storage/CDN thật, health check storage và bắt buộc public base URL hợp lệ trong production.

---

### P2-10: Backup production là opt-in, một lần và cùng host

**Mức độ:** Medium/High

Compose ghi rõ backup chỉ chạy thủ công và lưu local volume: [`docker-compose.prod.yml:333`](../docker-compose.prod.yml#L333).

Khuyến nghị:

- Lịch backup tự động.
- Mã hóa và đẩy off-host/object storage.
- Retention policy rõ ràng.
- Restore drill định kỳ và cảnh báo khi backup thất bại.

---

### P2-11: Container image/tag chưa reproducible hoàn toàn

**Mức độ:** Low/Medium

Production dùng các floating tag như `migrate/migrate:4`, `postgres:16-alpine`, `redis:7-alpine` và application `latest` mặc định: [`docker-compose.prod.yml:48`](../docker-compose.prod.yml#L48), [`docker-compose.prod.yml:232`](../docker-compose.prod.yml#L232).

Khuyến nghị pin version patch hoặc digest, đặc biệt cho production deployment.

---

### P2-12: Migration bot phá hủy dữ liệu nằm trong migration chain tự động

**Mức độ:** High operational risk

- Up migration chạy `DROP SCHEMA IF EXISTS bot CASCADE`: [`migrations/bot/000009_drop_bot_schema.up.sql:22`](../migrations/bot/000009_drop_bot_schema.up.sql#L22).
- Down migration chỉ tái tạo schema rỗng và không thể phục hồi dữ liệu: [`migrations/bot/000009_drop_bot_schema.down.sql:1`](../migrations/bot/000009_drop_bot_schema.down.sql#L1).
- Module bot vẫn nằm trong migration chain production: [`docker-compose.prod.yml:203`](../docker-compose.prod.yml#L203).

Migration có chủ ý và được document tốt, nhưng cần deployment gate riêng:

- Xác minh external bot đã tiếp nhận dữ liệu.
- Backup và kiểm thử restore trước migration.
- Approval thủ công cho production.
- Không coi down migration là data rollback.

---

### P2-13: Test coverage thiếu ở các vùng có rủi ro cao

**Mức độ:** High engineering risk

Các package hiện không có test đáng kể:

- `internal/feature/notification/broker`
- `internal/feature/notification/cache`
- `internal/feature/notification/service`
- `internal/feature/search/*`
- `internal/feature/storage/*`
- `pkg/errors`
- `pkg/jwt`
- `pkg/logger`
- `pkg/storage`

Redis timeline integration tests bị skip nếu `REDIS_TEST_ADDR` không được cấu hình: [`internal/feature/feed/cache/redis_timeline_store_test.go:26`](../internal/feature/feed/cache/redis_timeline_store_test.go#L26).

Khuyến nghị ưu tiên test theo rủi ro:

1. Authorization matrix cho post/comment/like.
2. Upload MIME validation và same-origin active content.
3. Storage factory fail-closed.
4. Error immutability và concurrent panic handling.
5. Feed exact-end cursor, tie-score pagination, queue-full và stale repair.
6. SSE publish/disconnect/shutdown dưới race detector.
7. Notification unread-cache consistency.
8. Concurrent refresh-token rotation.

## Thứ tự triển khai đề xuất

### Giai đoạn 1 — chặn release

1. Sửa authorization cho post/comment/like.
2. Khóa upload avatar/cover bằng content validation và tách upload origin.
3. Làm storage factory fail-closed; vô hiệu hóa tùy chọn S3 cho tới khi có implementation thật.
4. Làm error values immutable và thay panic response.

### Giai đoạn 2 — đảm bảo feed và notification đúng

1. Thêm durable outbox cho feed event.
2. Sửa timeline cursor handoff và Redis tie pagination.
3. Thêm timeline reconciliation/delete/visibility handling.
4. Sửa SSE broker concurrency.
5. Sửa unread notification cache consistency.

### Giai đoạn 3 — session và API hardening

1. Hash refresh token và atomic consume-and-rotate.
2. Bỏ query-string bearer token toàn cục.
3. Giới hạn user account detail cho self/admin.
4. Strict JSON decoder, body limit và security headers.
5. Pin exact JWT algorithm và validate issuer/audience.

### Giai đoạn 4 — production readiness

1. Object storage/CDN dùng chung cho multi-instance.
2. Automated off-host backup và restore drill.
3. Pin container versions/digests.
4. Gate migration phá hủy dữ liệu.
5. Bổ sung integration/load/race tests cho các vùng còn trống.

## Tiêu chí hoàn tất audit findings

Một issue chỉ nên được đóng khi:

- Có regression/characterization test thể hiện lỗi cũ.
- Fix được kiểm tra bằng `make test`, `go test -race ./...`, `go vet ./...` và `make lint`.
- API behavior hoặc deployment impact được cập nhật trong docs/PR.
- Các thay đổi package responsibility cập nhật `docs.go` nếu cần.
- Security-sensitive fix có test cả happy path và denied/abuse path.
