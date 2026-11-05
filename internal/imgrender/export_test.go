package imgrender

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
