package main

import (
	"bufio"
	"flag"
	"fmt"
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
	{`\[(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} [+-]\d{4})\]`, "2006-01-02 15:04:05 -0700"}, // New format
	{`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3})`, "2006-01-02 15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3})`, "2006-01-02 15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})`, "2006-01-02 15:04:05"},
	{`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2},\d{3})`, "2006-01-02T15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3})`, "2006-01-02T15:04:05.000"},
	{`(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})`, "2006-01-02T15:04:05"},
	{`([A-Za-z]{3} \d{2} \d{2}:\d{2}:\d{2})`, "Jan 02 15:04:05"},
	{`(\d{2}/[A-Za-z]{3}/\d{4} \d{2}:\d{2}:\d{2})`, "02/Jan/2006 15:04:05"},
	{`(\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})`, "02/Jan/2006:15:04:05 -0700"},
	{`(\d{2}:\d{2}:\d{2}\.\d{6})`, "15:04:05.000000"},
	{`(\d+) (\d{2}:\d{2}:\d{2}\.\d{6})`, "15:04:05.000000"}, // strace format
}

func parseLogLine(line string) (time.Time, string, error) {
	currentYear := time.Now().Year()
	for _, pattern := range timestampPatterns {
		re := regexp.MustCompile(pattern.regex)
		match := re.FindStringSubmatch(line)
		if match != nil {
			layout := pattern.layout
			timestampStr := match[1]
			if layout == "Jan 02 15:04:05" {
				timestampStr = fmt.Sprintf("%d %s", currentYear, timestampStr)
				layout = "2006 Jan 02 15:04:05"
			}
			timestamp, err := time.Parse(layout, timestampStr)
			if err != nil {
				return time.Time{}, "", err
			}

			// Find the index where the timestamp starts and ends
			startIndex := strings.Index(line, match[0])
			endIndex := startIndex + len(match[0])

			// Combine the parts before and after the timestamp
			restOfLine := line[:startIndex] + line[endIndex:]

			return timestamp, restOfLine, nil
		}
	}
	return time.Time{}, "", fmt.Errorf("no timestamp found in log line")
}

func readNextTimestamp(scanner *bufio.Scanner) (time.Time, string, error) {
	for scanner.Scan() {
		timestamp, restOfLine, err := parseLogLine(scanner.Text())
		if err == nil {
			return timestamp, restOfLine, nil
		}
	}
	return time.Time{}, "", fmt.Errorf("no more timestamps")
}

func getFilenamePrefix(filename string) string {
	// Get the last 20 characters of the filename
	if len(filename) > 20 {
		return filename[len(filename)-20:]
	}
	return filename
}

func main() {
	// Define command-line flags for start and end times
	startTimeStr := flag.String("start", "", "Start time (format: 2006-01-02T15:04:05)")
	endTimeStr := flag.String("end", "", "End time (format: 2006-01-02T15:04:05)")
	verbose := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	// Parse the start and end times
	var startTime, endTime time.Time
	var err error
	if *startTimeStr != "" {
		startTime, err = time.Parse("2006-01-02T15:04:05", *startTimeStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing start time: %v\n", err)
			os.Exit(1)
		}
	}
	if *endTimeStr != "" {
		endTime, err = time.Parse("2006-01-02T15:04:05", *endTimeStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing end time: %v\n", err)
			os.Exit(1)
		}
		endTime = endTime.Add(1 * time.Second)
	}

	// Get the remaining arguments (file patterns)
	files := flag.Args()
	if len(files) == 0 {
		fmt.Println("Usage: go run main.go [-start START_TIME] [-end END_TIME] <file1> <file2> ... <fileN>")
		os.Exit(1)
	}

	var allFiles []string
	for _, arg := range files {
		matches, err := filepath.Glob(arg)
		if err != nil {
			fmt.Printf("Error expanding glob pattern %s: %s\n", arg, err)
			continue
		}
		if len(matches) == 0 {
			fmt.Printf("No files match the pattern: %s\n", arg)
			continue
		}
		allFiles = append(allFiles, matches...)
	}

	if *verbose {
		fmt.Printf("Start time: %s\n", startTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("End time: %s\n", endTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("Files: %s\n", strings.Join(allFiles, "\n   "))
	}

	scanners := make([]*bufio.Scanner, len(allFiles))
	filenames := make([]string, len(allFiles))

	// Open all files and create scanners
	for i, file := range allFiles {
		f, err := os.Open(file)
		if err != nil {
			fmt.Printf("Error opening file %s: %s\n", file, err)
			continue
		}
		defer f.Close()
		scanners[i] = bufio.NewScanner(f)
		filenames[i] = filepath.Base(file)
	}

	timestamps := make([]time.Time, len(allFiles))
	restOfLines := make([]string, len(allFiles))
	errors := make([]error, len(allFiles))

	// Read the first timestamp from each file
	for i := range scanners {
		if scanners[i] != nil {
			timestamps[i], restOfLines[i], errors[i] = readNextTimestamp(scanners[i])
		}
	}

	for {
		var earliestIndex int
		var earliestTime time.Time
		found := false

		// Find the earliest timestamp
		for i, ts := range timestamps {
			if errors[i] == nil && (!found || ts.Before(earliestTime)) {
				earliestTime = ts
				earliestIndex = i
				found = true
			}
		}

		if !found {
			// No more timestamps
			break
		}

		if (startTime.IsZero() || !earliestTime.Before(startTime)) && (endTime.IsZero() || !earliestTime.After(endTime)) {
			// Print the earliest timestamp with the filename prefix and the rest of the line
			filenamePrefix := getFilenamePrefix(filenames[earliestIndex])
			fmt.Printf("%s: %s: %s\n", earliestTime.Format("2006-01-02 15:04:05"), filenamePrefix, restOfLines[earliestIndex])
		}
		if !endTime.IsZero() && earliestTime.After(endTime) {
			break
		}

		// Read the next timestamp from the file that had the earliest timestamp
		timestamps[earliestIndex], restOfLines[earliestIndex], errors[earliestIndex] = readNextTimestamp(scanners[earliestIndex])
	}
}
