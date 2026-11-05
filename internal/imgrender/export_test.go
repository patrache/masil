package imgrender

// Fit exposes fit to the external test package.
var Fit = fit

// SetCacheLimit sets the disk cache byte limit (default 200MB).
func SetCacheLimit(c *Cache, bytes int64) { c.setLimit(bytes) }

// SetRenderLimit sets the render LRU size (default 64).
func SetRenderLimit(r *Renderer, n int) { r.setLimit(n) }
