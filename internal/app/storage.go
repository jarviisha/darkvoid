package app

import (
	feathandler "github.com/jarviisha/darkvoid/internal/feature/storage/handler"
	featsvc "github.com/jarviisha/darkvoid/internal/feature/storage/service"
	"github.com/jarviisha/darkvoid/pkg/storage"
)

// StorageContext represents the Storage bounded context.
type StorageContext struct {
	MediaService *featsvc.MediaService
	MediaHandler *feathandler.MediaHandler
}

// SetupStorageContext initializes the Storage context.
func SetupStorageContext(store storage.Storage) *StorageContext {
	mediaService := featsvc.NewMediaService(store)
	mediaHandler := feathandler.NewMediaHandler(mediaService)

	return &StorageContext{
		MediaService: mediaService,
		MediaHandler: mediaHandler,
	}
}
