package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	botRepository "github.com/jarviisha/darkvoid/internal/feature/bot/repository"
)

func (app *Application) setupBotContext() {
	app.Bot = SetupBotContext(buildBotRepo(app.pool))
	app.Bot.WirePostReader(app.Post)
	app.log.Info("bot context initialized")
}

func buildBotRepo(pool *pgxpool.Pool) *botRepository.BotRepository {
	return botRepository.NewBotRepository(pool)
}
