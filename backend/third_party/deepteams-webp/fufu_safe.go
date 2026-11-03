package webp

// fufu: added for this fork (see FUFU.md). Not part of upstream deepteams/webp.

import (
	"fmt"
	"image"
	"io"
)

// safeDecode is Decode with any panic turned into an error.
func safeDecode(r io.Reader) (img image.Image, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			img, err = nil, fmt.Errorf("webp: decode panicked: %v", rec)
		}
	}()
	return Decode(r)
}

// safeDecodeConfig is DecodeConfig with any panic turned into an error.
func safeDecodeConfig(r io.Reader) (cfg image.Config, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			cfg, err = image.Config{}, fmt.Errorf("webp: decode config panicked: %v", rec)
		}
	}()
	return DecodeConfig(r)
}
