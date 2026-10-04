// Package download reports byte progress for streaming downloads.
package download

import "io"

// Progress describes one file. Total is zero when its size is unknown;
// Done means the response body reached EOF, not that the update was installed.
type Progress struct {
	Name            string
	Received, Total int64
	Done            bool
}

// Track reports the initial size and subsequent reads without buffering content.
// The callback runs in the reader's goroutine and should return promptly. When
// tracking concurrent downloads, the callback must be safe for concurrent use.
// A nil callback leaves the reader unchanged.
func Track(reader io.Reader, name string, total int64, report func(Progress)) io.Reader {
	if report == nil {
		return reader
	}
	progress := Progress{Name: name, Total: max(0, total)}
	report(progress)
	return &progressReader{reader: reader, progress: progress, report: report}
}

type progressReader struct {
	reader   io.Reader
	progress Progress
	report   func(Progress)
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.progress.Received += int64(n)
	r.progress.Done = err == io.EOF
	if n > 0 || err != nil {
		r.report(r.progress)
	}
	return n, err
}
