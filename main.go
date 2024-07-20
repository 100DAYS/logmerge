package main

import (
	"bufio"
	"errors"
	"flag"
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
	filename   string
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
var currentYear int = time.Now().Year()
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
	filenames := make([]string, len(allFiles))
	fileErrors := make([]error, len(allFiles))

	// Open all files and create scanners
	for i, file := range allFiles {
		f, err := os.Open(file)
		if err != nil {
			fileErrors[i] = err
			logErrorf("Error opening file %s: %s\n", file, err)
			continue
		}
		defer f.Close()
		scanners[i] = bufio.NewScanner(f)
		filenames[i] = filepath.Base(file)
	}

	timestamps := make([]time.Time, len(allFiles))
	restOfLines := make([]string, len(allFiles))

	// Read the first timestamp from each file
	for i := range scanners {
		if scanners[i] != nil {
			timestamps[i], restOfLines[i], fileErrors[i] = readNextTimestamp(scanners[i], i)
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
			ch <- lineStruct{earliestTime, filenames[earliestIndex], restOfLines[earliestIndex]}
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
					logWarnf("%s: %v\n", filenames[earliestIndex], err)
				}
				fileErrors[earliestIndex] = err // this will end Reading from the file
			}
			// in case of NoTimestampError, there is no timestamp in this line, so we keep the old timestamp
		}
	}
	close(ch)
}

func main() {
	// Define command-line flags for start and end times
	startTimeStr := flag.String("start", "", "Start time (format: 2006-01-02T15:04:05)")
	endTimeStr := flag.String("end", "", "End time (format: 2006-01-02T15:04:05)")
	fieldSeparator := flag.String("sep", " ", "Field separator")
	verbose := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	// Parse the start and end times
	var startTime, endTime time.Time
	var err error
	if *startTimeStr != "" {
		startTime, err = time.Parse("2006-01-02T15:04:05", *startTimeStr)
		if err != nil {
			logErrorf("Error parsing start time: %v\n", err)
			os.Exit(1)
		}
	}
	if *endTimeStr != "" {
		endTime, err = time.Parse("2006-01-02T15:04:05", *endTimeStr)
		if err != nil {
			logErrorf("Error parsing end time: %v\n", err)
			os.Exit(1)
		}
		endTime = endTime.Add(1 * time.Second)
	}

	// Get the remaining arguments (file patterns)
	files := flag.Args()
	if len(files) == 0 {
		_, _ = flag.CommandLine.Output().Write([]byte("No files specified\nUsage: logmerge [switches] <file1> <file2> ... <fileN>\nSwitches:\n"))
		flag.PrintDefaults()
		os.Exit(1)
	}

	profilingStart := time.Now()

	var allFiles []string
	for _, arg := range files {
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

	if *verbose {
		PrintfStderr("Start time: %s\n", startTime.Format("2006-01-02 15:04:05"))
		PrintfStderr("End time: %s\n", endTime.Format("2006-01-02 15:04:05"))
		PrintfStderr("Files: %s\n", strings.Join(allFiles, "\n   "))
	}

	ch := make(chan lineStruct)

	go mergeLogs(allFiles, startTime, endTime, *verbose, ch)

	for line := range ch {
		filenamePrefix := getFilenamePrefix(line.filename)
		fmt.Printf("%s%s%s%s%s\n", line.timestamp.Format("2006-01-02 15:04:05"), *fieldSeparator, filenamePrefix, *fieldSeparator, line.restOfLine)
	}

	if *verbose {
		PrintfStderr("Lines: %d\n", processedLines)
		PrintfStderr("Cache hits: %d\n", cacheHits)
		PrintfStderr("Duration %s\n", time.Since(profilingStart))
	}
}
