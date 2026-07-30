# Development Guidance

## Fyne UI Concurrency

- Perform UI changes from background work through `fyne.Do(...)` so they run on the Fyne UI thread. Do not mutate Fyne widgets directly from a worker.
- Keep the closure passed to `fyne.Do(...)` short: perform network requests, process execution, file I/O, and other blocking work before dispatching the UI update.
- Do not use `RunOnMain`; it is obsolete. `fyne.Do(...)` is the supported dispatch mechanism.