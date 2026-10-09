package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// MaxPreviewSize is the largest file a preview reads, in bytes; the listing's size decides before any download.
const MaxPreviewSize = 256 << 10

// Tree fetches the entries of dir ("" is the root) on ref ("" is the default branch), directories first; a missing dir or ref yields none, not an error.
func (s *Service) Tree(ctx context.Context, r domain.RepoRef, ref, dir string) ([]domain.TreeEntry, error) {
	tr, ok := s.f.(forge.TreeReader)
	if !ok {
		return nil, fmt.Errorf("files of %s: %w", r, forge.ErrUnsupported)
	}
	what := "list " + r.String() + " " + strconv.Quote(dir) + refSuffix(ref)
	return fetch(ctx, s, Key{Kind: KindTree, Repo: r, Ref: ref, Path: dir}, what,
		func(ctx context.Context) ([]domain.TreeEntry, error) {
			es, err := tr.ListTree(ctx, r, ref, dir)
			if errors.Is(err, forge.ErrNotFound) {
				return nil, nil
			}
			slices.SortFunc(es, compareEntries)
			return es, err
		})
}

// PeekTree returns the cached entries of dir on ref without I/O.
func (s *Service) PeekTree(r domain.RepoRef, ref, dir string) ([]domain.TreeEntry, time.Time, bool) {
	return peek[domain.TreeEntry](s, Key{Kind: KindTree, Repo: r, Ref: ref, Path: dir})
}

func refSuffix(ref string) string {
	if ref == "" {
		return ""
	}
	return " at " + ref
}

// compareEntries puts directories first, then orders by case-insensitive name, ties broken by exact name.
func compareEntries(a, b domain.TreeEntry) int {
	if ad, bd := a.Type == domain.EntryDir, b.Type == domain.EntryDir; ad != bd {
		if ad {
			return -1
		}
		return 1
	}
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}

// Preview fetches the preview of file entry e on ref ("" is the default branch); an entry over MaxPreviewSize is refused without a download.
func (s *Service) Preview(ctx context.Context, r domain.RepoRef, ref string, e domain.TreeEntry) (domain.FilePreview, error) {
	tr, ok := s.f.(forge.TreeReader)
	if !ok {
		return domain.FilePreview{}, fmt.Errorf("preview of %s in %s: %w", e.Path, r, forge.ErrUnsupported)
	}
	what := "preview of " + e.Path + " in " + r.String() + refSuffix(ref)
	vals, err := fetch(ctx, s, Key{Kind: KindPreview, Repo: r, Ref: ref, Path: e.Path}, what,
		func(ctx context.Context) ([]domain.FilePreview, error) {
			if e.Size > MaxPreviewSize {
				return []domain.FilePreview{{TooLarge: true}}, nil
			}
			b, err := tr.ReadFile(ctx, r, ref, e.Path)
			if err != nil {
				return nil, err
			}
			return []domain.FilePreview{toPreview(b)}, nil
		})
	if err != nil {
		return domain.FilePreview{}, err
	}
	return vals[0], nil
}

// PeekPreview returns the cached preview of the file at path on ref without I/O.
func (s *Service) PeekPreview(r domain.RepoRef, ref, path string) (domain.FilePreview, time.Time, bool) {
	vals, at, ok := peek[domain.FilePreview](s, Key{Kind: KindPreview, Repo: r, Ref: ref, Path: path})
	if !ok {
		return domain.FilePreview{}, time.Time{}, false
	}
	return vals[0], at, true
}

// toPreview classifies file bytes: over the cap, binary (a NUL or invalid UTF-8), or text.
func toPreview(b []byte) domain.FilePreview {
	switch {
	case len(b) > MaxPreviewSize:
		return domain.FilePreview{TooLarge: true}
	case bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b):
		return domain.FilePreview{Binary: true}
	}
	return domain.FilePreview{Text: string(b)}
}
