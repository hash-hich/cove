package erofs

import "context"

// Root exposes the root of the cache to the tests of the package: what they look into to see
// that a conversion filed what it should and left nothing else behind.
func (c *Cache) Root() string { return c.root }

// PlanWithCeiling exposes the plan drawn with the ceiling ceiling to the tests of the package: the
// composition of groups is the common path on x86_64 only, and lowering the ceiling by hand is
// what exercises it on a host that would never reach it.
func (c *Cache) PlanWithCeiling(ctx context.Context, img Image, layers []Blob, ceiling int) (Plan, error) {
	return c.planUnder(ctx, img, layers, ceiling, nil)
}
