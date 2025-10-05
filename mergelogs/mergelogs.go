package mergelogs

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type lineStruct struct {
	timestamp  time.Time
	fileIndex  int
	restOfLine string
}

var timestampPatterns = []struct {
	regex  *regexp.Regexp
	layout string
}{
	{regexp.MustCompile(`([A-Za-z]{3} +\d+ \d{2}:\d{2}:\d{2})`), "Jan _2 15:04:05"},
	{regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{4})`), "2006-01-02 15:04:05 -0700"},
	{regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3})`), "2006-01-02 15:04:05.000"},
	{regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})`), "2006-01-02 15:04:05.000"},
	{regexp.MustCompile(`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`), "2006-01-02 15:04:05"},
	{regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2},\d{3})`), "2006-01-02T15:04:05.000"},
	{regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3})`), "2006-01-02T15:04:05.000"},
	{regexp.MustCompile(`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})`), "2006-01-02T15:04:05"},
	{regexp.MustCompile(`(\d{2}/[A-Za-z]{3}/\d{4} \d{2}:\d{2}:\d{2})`), "02/Jan/2006 15:04:05"},
	{regexp.MustCompile(`(\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})`), "02/Jan/2006:15:04:05 -0700"},
	{regexp.MustCompile(`(\d{2}:\d{2}:\d{2}\.\d{6})`), "15:04:05.000000"},
	{regexp.MustCompile(`(\d{1,2}:\d{2}:\d{2}\.\d{6})`), "15:04:05.000000"},     // single or double-digit hour with microseconds
	{regexp.MustCompile(`(\d+) (\d{2}:\d{2}:\d{2}\.\d{6})`), "15:04:05.000000"}, // strace format
}

var NoTimestampError = errors.New("no Timestamp in Line")
var EndOfFileError = errors.New("end of file")

var logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

func findBestMatch(line string) (index int, resLoc []int, err error) {
	err = NoTimestampError
	for i, pattern := range timestampPatterns {
		loc := pattern.regex.FindStringIndex(line)
		if loc == nil {
			continue
		}
		if resLoc == nil || loc[0] < resLoc[0] || (loc[1]-loc[0] > resLoc[1]-resLoc[0]) {
			resLoc = loc
			index = i
			err = nil
		}
	}
	return
}

var logFormatIndexes = map[int]int{}
var currentYear = time.Now().Year()
var processedLines int
var cacheHits int

func extractTimestamp(line string, loc []int, layout string) (timestamp time.Time, remaining string, err error) {
	timestamp, err = time.Parse(layout, line[loc[0]:loc[1]])
	if err != nil {
		return time.Time{}, line, NoTimestampError
	}
	if timestamp.Year() == 0 {
		timestamp = timestamp.AddDate(currentYear, 0, 0)
	}
	return timestamp, line[:loc[0]] + line[loc[1]:], nil
}

func parseLogLine(line string, fileIndex int) (time.Time, string, error) {
	var loc []int
	var patternIndex int
	var err error

	processedLines++

	if idx, ok := logFormatIndexes[fileIndex]; ok {
		pattern := timestampPatterns[idx]
		loc = pattern.regex.FindStringIndex(line)
		if loc != nil {
			timestamp, remaining, err := extractTimestamp(line, loc, pattern.layout)
			if err == nil {
				cacheHits++
			}
			return timestamp, remaining, err
		}

	}

	patternIndex, loc, err = findBestMatch(line)
	if err == nil {
		timestamp, remaining, err := extractTimestamp(line, loc, timestampPatterns[patternIndex].layout)
		if err == nil {
			logFormatIndexes[fileIndex] = patternIndex
		}
		return timestamp, remaining, nil
	}
	return time.Time{}, line, NoTimestampError
}

func readNextTimestamp(scanner *bufio.Scanner, fileIndex int) (time.Time, string, error) {
	for scanner.Scan() {
		timestamp, restOfLine, err := parseLogLine(scanner.Text(), fileIndex)
		if err == nil {
			return timestamp, restOfLine, nil
		} else if err == NoTimestampError {
			return time.Time{}, restOfLine, NoTimestampError
		}
	}
	return time.Time{}, "", EndOfFileError
}

func readFirstTimestamp(scanner *bufio.Scanner, fileIndex int) (time.Time, string, error) {
	for scanner.Scan() {
		timestamp, restOfLine, err := parseLogLine(scanner.Text(), fileIndex)
		if err == nil {
			return timestamp, restOfLine, nil
		}
		// Skip lines without timestamps at the beginning of the file
	}
	return time.Time{}, "", EndOfFileError
}

func getFilenamePrefix(filename string) string {
	// Get the last 20 characters of the filename
	if len(filename) > 20 {
		return filename[len(filename)-20:]
	}
	return filename
}

func logErrorf(format string, args ...interface{}) {
	logger.Error(fmt.Sprintf(format, args...))
}

func logWarnf(format string, args ...interface{}) {
	logger.Warn(fmt.Sprintf(format, args...))
}

func PrintfStderr(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stderr, format, args...)
}

func mergeLogs(allFiles []string, startTime time.Time, endTime time.Time, verbose bool, ch chan<- lineStruct) {
	scanners := make([]*bufio.Scanner, len(allFiles))
	fileErrors := make([]error, len(allFiles))

	// Open all files and create scanners
	for i, file := range allFiles {
		f, err := os.Open(file)
		if err != nil {
			fileErrors[i] = err
			logErrorf("Error opening file %s: %s\n", file, err)
			continue
		}
		defer func() {
			err = f.Close()
			if err != nil {
				logErrorf("Error closing file %s: %s\n", file, err)
			}
		}()
		scanners[i] = bufio.NewScanner(f)
	}

	timestamps := make([]time.Time, len(allFiles))
	restOfLines := make([]string, len(allFiles))

	// Read the first timestamp from each file (skip initial lines without timestamps)
	for i := range scanners {
		if scanners[i] != nil {
			timestamps[i], restOfLines[i], fileErrors[i] = readFirstTimestamp(scanners[i], i)
		}
	}

	for {
		var earliestIndex int
		var earliestTime time.Time
		found := false

		// Find the earliest timestamp
		for i, ts := range timestamps {
			if fileErrors[i] == nil {
				if !found || ts.Before(earliestTime) {
					earliestTime = ts
					earliestIndex = i
					found = true
				}
			}
		}

		if !found {
			// No more timestamps
			break
		}

		if !endTime.IsZero() && earliestTime.After(endTime) {
			break
		}

		if startTime.IsZero() || !earliestTime.Before(startTime) {
			ch <- lineStruct{earliestTime, earliestIndex, restOfLines[earliestIndex]}
		}
		// Read the next timestamp from the file that had the earliest timestamp
		if fileErrors[earliestIndex] == nil {
			var newts time.Time
			var err error
			newts, restOfLines[earliestIndex], err = readNextTimestamp(scanners[earliestIndex], earliestIndex)
			if err == nil {
				timestamps[earliestIndex] = newts
				//fileErrors[earliestIndex] = nil
			} else if !errors.Is(err, NoTimestampError) {
				if verbose {
					logWarnf("%s: %v\n", allFiles[earliestIndex], err)
				}
				fileErrors[earliestIndex] = err // this will end Reading from the file
			}
			// in case of NoTimestampError, there is no timestamp in this line, so we keep the old timestamp
		}
	}
	close(ch)
}

// Options contains configuration for the merge operation
type Options struct {
	Files          []string
	StartTime      time.Time
	EndTime        time.Time
	FieldSeparator string
	Verbose        bool
}

// Stats contains statistics about the merge operation
type Stats struct {
	ProcessedLines int
	CacheHits      int
	Duration       time.Duration
}

// ProcessLogs processes log files according to the provided options and writes output to the provided writer
func ProcessLogs(opts Options, outputWriter *bufio.Writer) (*Stats, error) {
	profilingStart := time.Now()

	// Reset global counters
	processedLines = 0
	cacheHits = 0
	logFormatIndexes = map[int]int{}

	var allFiles []string
	for _, arg := range opts.Files {
		matches, err := filepath.Glob(arg)
		if err != nil {
			logErrorf("Error expanding glob pattern %s: %s\n", arg, err)
			continue
		}
		if len(matches) == 0 {
			logErrorf("No files match the pattern: %s\n", arg)
			continue
		}
		allFiles = append(allFiles, matches...)
	}

	if len(allFiles) == 0 {
		return nil, errors.New("no files to process")
	}

	filenames := make([]string, len(allFiles))
	for i, file := range allFiles {
		filenames[i] = getFilenamePrefix(filepath.Base(file))
	}

	if opts.Verbose {
		PrintfStderr("Start time: %s\n", opts.StartTime.Format("2006-01-02 15:04:05"))
		PrintfStderr("End time: %s\n", opts.EndTime.Format("2006-01-02 15:04:05"))
		PrintfStderr("Files: %s\n", strings.Join(allFiles, "\n   "))
	}

	ch := make(chan lineStruct)

	go mergeLogs(allFiles, opts.StartTime, opts.EndTime, opts.Verbose, ch)

	for line := range ch {
		fmt.Fprintf(outputWriter, "%s%s%s%s%s\n",
			line.timestamp.Format("2006-01-02 15:04:05"),
			opts.FieldSeparator,
			filenames[line.fileIndex],
			opts.FieldSeparator,
			line.restOfLine)
	}

	if err := outputWriter.Flush(); err != nil {
		return nil, fmt.Errorf("error flushing output: %w", err)
	}

	stats := &Stats{
		ProcessedLines: processedLines,
		CacheHits:      cacheHits,
		Duration:       time.Since(profilingStart),
	}

	return stats, nil
}