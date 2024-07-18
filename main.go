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

var timestampPatterns = []struct {
	regex  string
	layout string
}{
	{`([A-Za-z]{3} +\d+ \d{2}:\d{2}:\d{2})`, "Jan _2 15:04:05"},
	{`\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{4})\]`, "2006-01-02 15:04:05 -0700"}, // New format
	{`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3})`, "2006-01-02 15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})`, "2006-01-02 15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`, "2006-01-02 15:04:05"},
	{`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2},\d{3})`, "2006-01-02T15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3})`, "2006-01-02T15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})`, "2006-01-02T15:04:05"},
	{`(\d{2}/[A-Za-z]{3}/\d{4} \d{2}:\d{2}:\d{2})`, "02/Jan/2006 15:04:05"},
	{`(\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})`, "02/Jan/2006:15:04:05 -0700"},
	{`(\d{2}:\d{2}:\d{2}\.\d{6})`, "15:04:05.000000"},
	{`(\d+) (\d{2}:\d{2}:\d{2}\.\d{6})`, "15:04:05.000000"}, // strace format
}

var NoTimestampError = errors.New("no Timestamp in Line")
var EndOfFileError = errors.New("end of file")
var logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

func parseLogLine(line string) (time.Time, string, error) {
	currentYear := time.Now().Year()
	for _, pattern := range timestampPatterns {
		re := regexp.MustCompile(pattern.regex)
		match := re.FindStringSubmatch(line)
		if match != nil {
			layout := pattern.layout
			timestampStr := match[1]
			timestamp, err := time.Parse(layout, timestampStr)
			if err != nil {
				return time.Time{}, "", err
			}
			if timestamp.Year() == 0 {
				timestamp = timestamp.AddDate(currentYear, 0, 0)
			}

			// Find the index where the timestamp starts and ends
			startIndex := strings.Index(line, match[0])
			endIndex := startIndex + len(match[0])

			// Combine the parts before and after the timestamp
			restOfLine := line[:startIndex] + line[endIndex:]

			return timestamp, restOfLine, nil
		}
	}
	return time.Time{}, line, NoTimestampError
}

func readNextTimestamp(scanner *bufio.Scanner) (time.Time, string, error) {
	for scanner.Scan() {
		timestamp, restOfLine, err := parseLogLine(scanner.Text())
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
		//fmt.Println("Usage: logmerge [-v] [-sep FIELD_SEPARATOR] [-start START_TIME] [-end END_TIME] <file1> <file2> ... <fileN>")
		os.Exit(1)
	}

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
			timestamps[i], restOfLines[i], fileErrors[i] = readNextTimestamp(scanners[i])
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

		if (startTime.IsZero() || !earliestTime.Before(startTime)) && (endTime.IsZero() || !earliestTime.After(endTime)) {
			// Print the earliest timestamp with the filename prefix and the rest of the line
			filenamePrefix := getFilenamePrefix(filenames[earliestIndex])
			fmt.Printf("%s%s%s%s%s\n", earliestTime.Format("2006-01-02 15:04:05"), *fieldSeparator, filenamePrefix, *fieldSeparator, restOfLines[earliestIndex])
		}
		if !endTime.IsZero() && earliestTime.After(endTime) {
			break
		}

		// Read the next timestamp from the file that had the earliest timestamp
		if fileErrors[earliestIndex] == nil {
			var newts time.Time
			var err error
			newts, restOfLines[earliestIndex], err = readNextTimestamp(scanners[earliestIndex])
			if err == nil {
				timestamps[earliestIndex] = newts
				fileErrors[earliestIndex] = err
			} else if errors.Is(err, NoTimestampError) {
				// no timestamp in this line, keep the old timestamp
				fileErrors[earliestIndex] = nil
			} else {
				logWarnf("%s: %v\n", filenames[earliestIndex], err)
				fileErrors[earliestIndex] = err
			}
		}
	}
}
