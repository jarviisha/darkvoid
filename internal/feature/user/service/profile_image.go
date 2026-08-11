package service

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	user "github.com/jarviisha/darkvoid/internal/feature/user"
)

const (
	maxProfileImageSize   int64 = 10 << 20
	maxProfileImagePixels int64 = 25_000_000
)

type validatedProfileImage struct {
	reader      io.Reader
	contentType string
	extension   string
	size        int64
}

func validateProfileImage(r io.Reader, declaredSize int64) (*validatedProfileImage, error) {
	if declaredSize > maxProfileImageSize {
		return nil, user.ErrProfileImageTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(r, maxProfileImageSize+1))
	if err != nil {
		return nil, user.ErrUnsupportedImageType
	}
	if int64(len(data)) > maxProfileImageSize {
		return nil, user.ErrProfileImageTooLarge
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxProfileImagePixels {
		return nil, user.ErrUnsupportedImageType
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil || decodedFormat != format {
		return nil, user.ErrUnsupportedImageType
	}

	var sanitized bytes.Buffer
	contentType := ""
	extension := ""
	switch format {
	case "jpeg":
		contentType = "image/jpeg"
		extension = ".jpg"
		err = jpeg.Encode(&sanitized, decoded, &jpeg.Options{Quality: 90})
	case "png":
		contentType = "image/png"
		extension = ".png"
		err = png.Encode(&sanitized, decoded)
	default:
		return nil, user.ErrUnsupportedImageType
	}
	if err != nil {
		return nil, fmt.Errorf("encode sanitized profile image: %w", err)
	}

	return &validatedProfileImage{
		reader:      bytes.NewReader(sanitized.Bytes()),
		contentType: contentType,
		extension:   extension,
		size:        int64(sanitized.Len()),
	}, nil
}
