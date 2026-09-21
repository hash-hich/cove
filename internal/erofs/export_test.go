package erofs

// Root exposes the root of the cache to the tests of the package: what they look into to see
// that a conversion filed what it should and left nothing else behind.
func (c *Cache) Root() string { return c.root }
