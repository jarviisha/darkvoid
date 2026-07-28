package app

import (
	botHandler "github.com/jarviisha/darkvoid/internal/feature/bot/handler"
	botRepository "github.com/jarviisha/darkvoid/internal/feature/bot/repository"
	botService "github.com/jarviisha/darkvoid/internal/feature/bot/service"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
)

// botRoleName is the role required by the /api/v1/bot agent plane. It is held by a
// single runner account, not by the persona accounts — see the bot handler package
// docs. The RoleChecker middleware works in plain strings, so unwrap the constant.
const botRoleName = string(entity.RoleBot)

// BotContext holds all dependencies for the bot bounded context.
type BotContext struct {
	botRepo    *botRepository.BotRepository
	botService *botService.BotService
	botHandler *botHandler.BotHandler
}

// SetupBotContext initializes the bot context.
func SetupBotContext(repo *botRepository.BotRepository) *BotContext {
	svc := botService.NewBotService(repo)
	h := botHandler.NewBotHandler(svc)

	return &BotContext{
		botRepo:    repo,
		botService: svc,
		botHandler: h,
	}
}

// WirePostReader attaches the source of post previews for the activity log. Called
// after setup, because the bot context cannot depend on the post context at
// construction time.
func (ctx *BotContext) WirePostReader(post *PostContext) {
	ctx.botService.WithPostReader(&botPostReader{postRepo: post.Ports().FeedPostRepo})
}
