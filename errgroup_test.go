// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package errgroup_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"go.linka.cloud/errgroup"
)

var (
	Web   = fakeSearch("web")
	Image = fakeSearch("image")
	Video = fakeSearch("video")
)

type Result string
type Search func(ctx context.Context, query string) (Result, error)

func fakeSearch(kind string) Search {
	return func(ctx context.Context, query string) (Result, error) {
		t := time.After(time.Second)
		select {
		case <-t:
			return Result(fmt.Sprintf("%s result for %q", kind, query)), nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// Parallel illustrates the use of a group for synchronizing a simple parallel
// task: the "Google Search 2.0" function from
// https://talks.golang.org/2012/concurrency.slide#46, augmented with a Context
// and error-handling.
func ExampleGroup_parallel() {
	Google := func(ctx context.Context, query string) ([]Result, error) {
		g := errgroup.New(ctx)

		searches := []Search{Web, Image, Video}
		results := make([]Result, len(searches))
		for i, search := range searches {
			i, search := i, search // https://golang.org/doc/faq#closures_and_goroutines
			g.Go(func(ctx context.Context) error {
				result, err := search(ctx, query)
				if err == nil {
					results[i] = result
				}
				return err
			})
		}
		if err := g.Wait(); err != nil {
			return nil, err
		}
		return results, nil
	}

	results, err := Google(context.Background(), "golang")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	for _, result := range results {
		fmt.Println(result)
	}

	// Output:
	// web result for "golang"
	// image result for "golang"
	// video result for "golang"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.AfterFunc(100*time.Millisecond, cancel)
	}()
	results, err = Google(ctx, "golang")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
}

func TestZeroGroup(t *testing.T) {
	err1 := errors.New("errgroup_test: 1")
	err2 := errors.New("errgroup_test: 2")

	cases := []struct {
		errs []error
	}{
		{errs: []error{}},
		{errs: []error{nil}},
		{errs: []error{err1}},
		{errs: []error{err1, nil}},
		{errs: []error{err1, nil, err2}},
	}

	for _, tc := range cases {
		g := errgroup.New(context.Background())

		var firstErr error
		for i, err := range tc.errs {
			err := err
			g.Go(func(_ context.Context) error { return err })

			if firstErr == nil && err != nil {
				firstErr = err
			}

			if gErr := g.Wait(); gErr != firstErr {
				t.Errorf("after %T.Go(func() error { return err }) for err in %v\n"+
					"g.Wait() = %v; want %v",
					g, tc.errs[:i+1], err, firstErr)
			}
		}
	}
}

func TestWithContext(t *testing.T) {
	errDoom := errors.New("group_test: doomed")

	cases := []struct {
		errs []error
		want error
	}{
		{want: nil},
		{errs: []error{nil}, want: nil},
		{errs: []error{errDoom}, want: errDoom},
		{errs: []error{errDoom, nil}, want: errDoom},
	}

	for _, tc := range cases {
		g := errgroup.New(context.Background())

		for _, err := range tc.errs {
			err := err
			g.Go(func(_ context.Context) error { return err })
		}

		if err := g.Wait(); err != tc.want {
			t.Errorf("after %T.Go(func() error { return err }) for err in %v\n"+
				"g.Wait() = %v; want %v",
				g, tc.errs, err, tc.want)
		}

		canceled := false
		select {
		case <-g.Done():
			canceled = true
		default:
		}
		if !canceled {
			t.Errorf("after %T.Go(func() error { return err }) for err in %v\n"+
				"ctx.Done() was not closed",
				g, tc.errs)
		}
	}
}

func TestTryGo(t *testing.T) {
	g := errgroup.New(context.Background())
	n := 42
	g.SetLimit(42)
	ch := make(chan struct{})
	fn := func(_ context.Context) error {
		ch <- struct{}{}
		return nil
	}
	for i := 0; i < n; i++ {
		if !g.TryGo(fn) {
			t.Fatalf("TryGo should succeed but got fail at %d-th call.", i)
		}
	}
	if g.TryGo(fn) {
		t.Fatalf("TryGo is expected to fail but succeeded.")
	}
	go func() {
		for i := 0; i < n; i++ {
			<-ch
		}
	}()
	g.Wait()

	if !g.TryGo(fn) {
		t.Fatalf("TryGo should success but got fail after all goroutines.")
	}
	go func() { <-ch }()
	g.Wait()

	// Switch limit.
	g.SetLimit(1)
	if !g.TryGo(fn) {
		t.Fatalf("TryGo should success but got failed.")
	}
	if g.TryGo(fn) {
		t.Fatalf("TryGo should fail but succeeded.")
	}
	go func() { <-ch }()
	g.Wait()

	// Block all calls.
	g.SetLimit(0)
	for i := 0; i < 1<<10; i++ {
		if g.TryGo(fn) {
			t.Fatalf("TryGo should fail but got succeded.")
		}
	}
	g.Wait()
}

func TestGoLimit(t *testing.T) {
	const limit = 10

	g := errgroup.New(context.Background())
	g.SetLimit(limit)
	var active int32
	for i := 0; i <= 1<<10; i++ {
		g.Go(func(_ context.Context) error {
			n := atomic.AddInt32(&active, 1)
			if n > limit {
				return fmt.Errorf("saw %d active goroutines; want ??? %d", n, limit)
			}
			time.Sleep(1 * time.Microsecond) // Give other goroutines a chance to increment active.
			atomic.AddInt32(&active, -1)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkGo(b *testing.B) {
	fn := func(_ context.Context) {}
	g := errgroup.New(context.Background())
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		g.Go(func(ctx context.Context) error { fn(ctx); return nil })
	}
	g.Wait()
}
