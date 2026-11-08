package imgrender

import (
	"bytes"
	"math"
)

// Fit exposes fit to the external test package.
var Fit = fit

// SetCacheLimit sets the disk cache byte limit (default 200MB).
func SetCacheLimit(c *Cache, bytes int64) { c.setLimit(bytes) }

// SetRenderLimit sets the render LRU size (default 64).
func SetRenderLimit(r *Renderer, n int) { r.setLimit(n) }

// FitCalls returns how many times an image has been scaled into a pixel box.
func FitCalls() int64 { return fitCalls.Load() }

// MaxDisplaySide is the long-side cap of images kept by Cache.Load.
const MaxDisplaySide = maxDisplaySide

// SetMemLimit sets the memory LRU's pixel byte budget (default 256MB).
func SetMemLimit(c *Cache, bytes int64) { c.setMemLimit(bytes) }

// ImageBytes is the memory LRU's size estimate of img.
var ImageBytes = imageBytes

// CheckHeader runs the pre-decode header check with the display limits
// (Cache.Load) or the upload limits (Decode, PrepareUpload).
func CheckHeader(data []byte, ct string, display bool) error {
	lim := uploadLimits
	if display {
		lim = displayLimits
	}
	return checkConfig(codecs[ct], data, ct, lim)
}

// DecodePeak is the most decodes c ever ran at once.
func DecodePeak(c *Cache) int32 { return c.decodePeak.Load() }

// MaxDecodes is how many decodes a Cache runs at once.
const MaxDecodes = maxDecodes

// DecodeCost is the pre-decode estimate of the bytes decoding data
// allocates, worked out in full (no byte limit).
func DecodeCost(data []byte, ct string) (int64, error) {
	cfg, err := codecs[ct].config(bytes.NewReader(data))
	if err != nil {
		return 0, err
	}
	return decodeCost(ct, data, cfg, math.MaxInt64)
}

// DisplayImage is the shrink Cache.Load applies after decoding.
var DisplayImage = displayImage

// DownscaleToFit is PrepareUpload's re-encode-and-shrink loop.
var DownscaleToFit = downscaleToFit
