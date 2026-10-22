package download

import "context"

type progressKey struct{}

// WithProgress makes Report calls under ctx reach fn. The site clients stay unaware of the queue:
// the file writer reports through the request's context.
func WithProgress(ctx context.Context, fn func(done, total int64)) context.Context {
	return context.WithValue(ctx, progressKey{}, fn)
}

// Report passes the bytes written so far and the expected size (0 when unknown) to the WithProgress callback, if any.
func Report(ctx context.Context, done, total int64) {
	if fn, ok := ctx.Value(progressKey{}).(func(done, total int64)); ok {
		fn(done, total)
	}
}
