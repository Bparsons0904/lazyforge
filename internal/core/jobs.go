package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"git.bobparsons.dev/deadstyle/lazyforge/internal/domain"
	"git.bobparsons.dev/deadstyle/lazyforge/internal/forge"
)

// LogKeep caps the tail of a log JobLog returns; a run's log is unbounded and the pane shows only its end.
const LogKeep = 256 << 10

// Jobs is never cached, because a running run's jobs change under the caller.
// The error matches forge.ErrUnsupported when the forge can't list jobs.
func (s *Service) Jobs(ctx context.Context, r domain.RepoRef, runID int64) ([]domain.Job, error) {
	what := fmt.Sprintf("list jobs of run %d in %s", runID, r)
	rl, ok := s.f.(forge.RunLister)
	if !ok {
		return nil, fmt.Errorf("%s: %w", what, forge.ErrUnsupported)
	}
	var js []domain.Job
	err := s.do(ctx, what, func(ctx context.Context) (err error) {
		js, err = rl.ListJobs(ctx, r, runID)
		return err
	})
	return js, err
}

// JobLog keeps the last LogKeep bytes of the log on a line boundary. It is never cached.
// The error matches forge.ErrUnsupported when the forge can't read logs.
func (s *Service) JobLog(ctx context.Context, r domain.RepoRef, jobID int64) (string, error) {
	what := fmt.Sprintf("read log of job %d in %s", jobID, r)
	lr, ok := s.f.(forge.LogReader)
	if !ok {
		return "", fmt.Errorf("%s: %w", what, forge.ErrUnsupported)
	}
	var out string
	err := s.do(ctx, what, func(ctx context.Context) error {
		rc, err := lr.JobLog(ctx, r, jobID)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		b, err := readTail(rc, LogKeep)
		out = tailLines(string(b), LogKeep)
		return err
	})
	return out, err
}

// readTail reads r to the end but holds only about its last n bytes, so a huge log costs bounded memory.
func readTail(r io.Reader, n int) ([]byte, error) {
	var buf []byte
	chunk := make([]byte, 64<<10)
	for {
		k, err := r.Read(chunk)
		buf = append(buf, chunk[:k]...)
		if len(buf) > 2*n {
			buf = append(buf[:0], buf[len(buf)-n:]...)
		}
		if errors.Is(err, io.EOF) {
			return buf, nil
		}
		if err != nil {
			return buf, err
		}
	}
}

// tailLines returns the last n bytes of s, starting at a line boundary and marked when anything was dropped.
func tailLines(s string, n int) string {
	if len(s) <= n {
		return s
	}
	t := s[len(s)-n:]
	if i := strings.IndexByte(t, '\n'); i >= 0 {
		t = t[i+1:]
	} else {
		t = strings.ToValidUTF8(t, "")
	}
	return "… earlier output omitted\n" + t
}

// FirstProblemJob returns the index of the job to show first: the first failed one, else the first still running,
// else the last. It is 0 for an empty list.
func FirstProblemJob(js []domain.Job) int {
	for _, want := range []domain.CIState{domain.CIFail, domain.CIRunning} {
		if i := slices.IndexFunc(js, func(j domain.Job) bool { return j.Status == want }); i >= 0 {
			return i
		}
	}
	return max(len(js)-1, 0)
}
