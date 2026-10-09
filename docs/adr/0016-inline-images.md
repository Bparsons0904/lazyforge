# 0016. Inline images

- Status: accepted
- Date: 2026-10-08
- Decided by: Opus (second opinion: none; Fable not consulted: the placeholder approach is backed by a prototype and is reversible)

## Context

Ticket #60. Forge bodies carry images that sit behind the forge's login, so a plain HTTP client can't fetch them. ADR 0014 shows them as a `🖼 alt` link. The PM's answers: images come only from the session's own forge; an image fits the pane width and is capped at the visible height; kitty and Ghostty are detected automatically from an allowlist; a "Show images" setting is on by default; tmux without `allow-passthrough` falls back silently; the link shows while an image loads and when it fails.

## Decision

- **Drawing uses kitty graphics Unicode placeholders.** Each image is transmitted once with `U=1`. Its cells carry the image ID as a `38;5;N` foreground, plus row and column diacritics. The terminal draws the image into those cells, so images scroll and clip as text through Bubble Tea's renderer. This needs ANSI256 colour or better.
- **`internal/ui/termimg` (#66) owns the escape sequences, the image ID allocator and terminal detection.** It makes `golang.org/x/ansi` a direct dependency there.
- **Fetching goes through `forge.AssetReader`.** The gitea adapter's `OpenAsset` sends `Authorization` only to the session's origin, under `forge.SameOrigin` (scheme, hostname and port, with the scheme's default port filled in). It strips the header on every redirect hop off that origin. Go's own rule is not enough: it forwards the header to the same hostname on any port, to subdomains, and from https to http.
- **`core.Service.Image` and `PeekImage` do the work.** They enforce a 20 s timeout, 10 MiB (`ErrImageTooLarge`), 40 MP checked from the header before decoding (`ErrImageTooLarge`), and PNG, JPEG and GIF (first frame). Images are box-filtered to a 1600 px longest side. A 16-entry LRU caches both images and failures. Its entries are cleared by `ClearImages`, which the `r` key calls. The five-minute refresh does not clear it. This is an exception to ADR 0007's "entries never expire": refetching every on-screen image on a timer is wasted traffic for content that rarely changes.
- **`internal/ui/markdown` stays pure.** `Images` lists the image destinations in a body without I/O. `RenderWithImages` renders with placeholder blocks the caller has already prepared, and does no I/O. The integration ticket fixes both signatures.

## Consequences

Images draw in capable terminals and fall back to the `🖼 alt` link elsewhere, while loading, and on failure. Failures are cached until `r`, so a broken image is not retried on every render. One body can cost up to about 160 MB while a 40 MP image decodes; that is accepted, since callers request only on-screen images. The image cache holds at most 16 decoded images. Terminal output, detection and the markdown and details integration are later tickets.

## Alternatives

- **Direct placement without placeholders.** Rejected: the image would be drawn over text, so it wouldn't scroll or clip with the pane.
- **Sixel.** Not chosen: it draws pixels directly, so it can't scroll or clip with the text around it the way placeholders do.
- **A separate image view.** Not chosen: the PM's answers place images in the details pane, inline with the body.
- **`golang.org/x/image/draw` for scaling.** Rejected: the box filter is a few lines of stdlib code, and a new module needs its own decision.
