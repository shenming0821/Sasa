package ui

import "context"

func (a *App) runFileTask(fn func()) {
	a.runFileTaskContext(context.Background(), fn)
}

func (a *App) runFileTaskContext(ctx context.Context, fn func()) {
	a.runFileTaskWith(ctx, false, fn)
}

func (a *App) runBackgroundFileTaskContext(ctx context.Context, fn func()) {
	a.runFileTaskWith(ctx, true, fn)
}

func (a *App) runFileTaskWith(ctx context.Context, background bool, fn func()) {
	if a.fileWorkers == nil {
		a.fileWorkers = make(chan struct{}, 2)
	}
	if background && a.fileBackgroundWorkers == nil {
		// Leave one of the two IO slots available for an explicit file open.
		a.fileBackgroundWorkers = make(chan struct{}, 1)
	}
	workers := a.fileWorkers
	backgroundWorkers := a.fileBackgroundWorkers
	go func() {
		if background {
			select {
			case backgroundWorkers <- struct{}{}:
			case <-ctx.Done():
				fn()
				return
			case <-a.shutdown:
				return
			}
			defer func() { <-backgroundWorkers }()
		}
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			// Cancellable readers check ctx before IO and still clear their state.
			fn()
			return
		case <-a.shutdown:
			return
		}
		defer func() { <-workers }()
		fn()
	}()
}
func (a *App) runGitTaskContext(ctx context.Context, fn func()) {
	if a.gitWorkers == nil {
		a.gitWorkers = make(chan struct{}, 2)
	}
	workers := a.gitWorkers
	go func() {
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			fn()
			return
		case <-a.shutdown:
			return
		}
		defer func() { <-workers }()
		fn()
	}()
}
func (a *App) runHighlightTask(ctx context.Context, fn func()) {
	if a.highlightWorkers == nil {
		a.highlightWorkers = make(chan struct{}, 1)
	}
	workers := a.highlightWorkers
	go func() {
		select {
		case workers <- struct{}{}:
		case <-ctx.Done():
			// The callback observes cancellation and releases the document state.
			fn()
			return
		case <-a.shutdown:
			return
		}
		defer func() { <-workers }()
		fn()
	}()
}
