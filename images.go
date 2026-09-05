package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp"
)

const imageLimit = 5 << 20
const pixelLimit = 20_000_000

type storedImage struct {
	Name, MIME    string
	Bytes         int64
	Width, Height int
}

// Only one image is decoded at a time, bounding peak upload memory independently
// of the number of simultaneous callers. Request bodies are also read in this gate.
func validateImage(data []byte) (*storedImage, error) {
	if len(data) == 0 || len(data) > imageLimit {
		return nil, errors.New("image must be between 1 byte and 5 MiB")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("invalid image; use JPEG, PNG, WebP, or GIF")
	}
	mimes := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp", "gif": "image/gif"}
	mime, ok := mimes[format]
	if !ok {
		return nil, errors.New("unsupported image format")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > pixelLimit {
		return nil, errors.New("image exceeds 20 megapixels")
	}
	if format == "gif" {
		if err = checkGIFBudget(data); err != nil {
			return nil, err
		}
		_, err = gif.DecodeAll(bytes.NewReader(data))
	} else {
		_, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return nil, errors.New("image is corrupt or uses an unsupported encoding")
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	ext := format
	if format == "jpeg" {
		ext = "jpg"
	}
	return &storedImage{Name: hex.EncodeToString(random[:]) + "." + ext, MIME: mime, Bytes: int64(len(data)), Width: cfg.Width, Height: cfg.Height}, nil
}

// Preflight GIF blocks before DecodeAll allocates frames. Animated GIFs share
// the 20 MP decoded-pixel budget and are limited to 1,000 frames.
func checkGIFBudget(data []byte) error {
	bad := errors.New("invalid GIF")
	if len(data) < 13 {
		return bad
	}
	offset := 13
	if data[10]&128 != 0 {
		offset += 3 * (1 << ((data[10] & 7) + 1))
	}
	skipBlocks := func() bool {
		for offset < len(data) {
			n := int(data[offset])
			offset++
			if n == 0 {
				return true
			}
			offset += n
		}
		return false
	}
	var pixels int64
	frames := 0
	for offset < len(data) {
		kind := data[offset]
		offset++
		switch kind {
		case 0x3b:
			if frames == 0 {
				return bad
			}
			return nil
		case 0x21:
			offset++
			if !skipBlocks() {
				return bad
			}
		case 0x2c:
			if offset+9 > len(data) {
				return bad
			}
			w := int64(binary.LittleEndian.Uint16(data[offset+4:]))
			h := int64(binary.LittleEndian.Uint16(data[offset+6:]))
			pixels += w * h
			frames++
			if pixels > pixelLimit || frames > 1000 {
				return errors.New("animated GIF exceeds 20 million frame pixels or 1,000 frames")
			}
			packed := data[offset+8]
			offset += 9
			if packed&128 != 0 {
				offset += 3 * (1 << ((packed & 7) + 1))
			}
			offset++
			if !skipBlocks() {
				return bad
			}
		default:
			return bad
		}
	}
	return bad
}

func (s *Store) saveImage(ctx context.Context, img *storedImage, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := filepath.Join(s.dir, "images", img.Name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		os.Remove(path)
		return err
	}
	defer dir.Close()
	if err = dir.Sync(); err != nil {
		os.Remove(path)
		return fmt.Errorf("sync image directory: %w", err)
	}
	return nil
}
