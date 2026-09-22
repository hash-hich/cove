package erofs

// Apply exposes apply to the tests of the package: the conversion of one layer without the cache
// around it, which is what the fuzzing and the refusal of a writer that panics exercise.
var Apply = apply

// BlockSize exposes blockSize to the tests of the package.
const BlockSize = blockSize

// Root exposes the root of the cache to the tests of the package: what they look into to see
// that a conversion filed what it should and left nothing else behind.
func (c *Cache) Root() string { return c.root }
