package main

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

// promptData turns the persona into the value a prompt template renders against,
// combined with the subject drawn for this post and the repetition guard.
func (p persona) promptData(topic string, recent []string, maxTags int) promptData {
	return promptData{
		Username:    p.username,
		DisplayName: p.displayName,
		Style:       p.style,
		Topic:       topic,
		Recent:      recent,
		MaxTags:     maxTags,
	}
}
