# errgroup

Adapted from [golang.org/x/sync/errgroup](https://godoc.org/golang.org/x/sync/errgroup).

## API

```go
type Group interface {
	Go(f func(ctx context.Context) error)
	TryGo(f func(ctx context.Context) error) bool
	SetLimit(n int)
	Cancel() error
	Done() <-chan struct{}
	Wait() error
}

func New(ctx context.Context) Group
```
