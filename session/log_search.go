package session

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// LogSearchPageSize bounds the number of matching entries loaded into memory.
const LogSearchPageSize = 400

// LogSearchResult contains a bounded page of matches from the complete archive.
// Skip counts matches after the page, so zero follows the most recent matches.
type LogSearchResult struct {
	Lines   []string
	Matches int
	Skip    int
}

// SearchLogs scans only the current session's archive, with constant memory.
// A negative skip selects the first page. Scans stop at the size captured at open.
func SearchLogs(ctx context.Context, path, query string, skip int) (LogSearchResult, error) {
	var result LogSearchResult
	file, err := openLogFile(path)
	if err != nil {
		return result, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return result, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	scan := func(visit func(string)) error {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		scanner := bufio.NewScanner(io.LimitReader(file, info.Size()))
		scanner.Buffer(make([]byte, 8192), 16384)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return err
			}
			line := scanner.Text()
			if strings.Contains(strings.ToLower(line), query) {
				visit(line)
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return scanner.Err()
	}
	if err := scan(func(string) { result.Matches++ }); err != nil {
		return result, err
	}
	if skip < 0 {
		skip = max(0, result.Matches-1) / LogSearchPageSize * LogSearchPageSize
	}
	result.Skip = min(max(0, skip), max(0, result.Matches-1))
	end := result.Matches - result.Skip
	start := max(0, end-LogSearchPageSize)
	index := 0
	err = scan(func(line string) {
		if index >= start && index < end {
			result.Lines = append(result.Lines, line)
		}
		index++
	})
	return result, err
}
