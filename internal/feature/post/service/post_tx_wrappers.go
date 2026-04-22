package service

import (
	"github.com/jackc/pgx/v5"
	"github.com/jarviisha/darkvoid/internal/feature/post/repository"
)

// postRepoTxable wraps *repository.PostRepository so that its WithTx method
// can return the postRepo interface (defined in interfaces.go) without creating
// an import cycle.
type postRepoTxable struct{ *repository.PostRepository }

func (r *postRepoTxable) WithTx(tx pgx.Tx) postRepo {
	return &postRepoTxable{r.PostRepository.WithTx(tx)}
}

// mediaRepoTxable wraps *repository.MediaRepository for the same reason.
type mediaRepoTxable struct{ *repository.MediaRepository }

func (r *mediaRepoTxable) WithTx(tx pgx.Tx) mediaRepo {
	return &mediaRepoTxable{r.MediaRepository.WithTx(tx)}
}

// hashtagRepoTxable wraps *repository.HashtagRepository so that its WithTx method
// can return the hashtagRepo interface without creating an import cycle.
type hashtagRepoTxable struct{ *repository.HashtagRepository }

func (r *hashtagRepoTxable) WithTx(tx pgx.Tx) hashtagRepo {
	return &hashtagRepoTxable{r.HashtagRepository.WithTx(tx)}
}

// mentionRepoTxable wraps *repository.MentionRepository so that its WithTx method
// can return the mentionRepo interface without creating an import cycle.
type mentionRepoTxable struct{ *repository.MentionRepository }

func (r *mentionRepoTxable) WithTx(tx pgx.Tx) mentionRepo {
	return &mentionRepoTxable{r.MentionRepository.WithTx(tx)}
}
