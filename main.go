package main

import (
	"bufio"
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
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run main.go <file1> <file2> ... <fileN>")
		return
	}

	var files []string
	for _, arg := range os.Args[1:] {
		matches, err := filepath.Glob(arg)
		if err != nil {
			fmt.Printf("Error expanding glob pattern %s: %s\n", arg, err)
			continue
		}
		if len(matches) == 0 {
			fmt.Printf("No files match the pattern: %s\n", arg)
			continue
		}
		files = append(files, matches...)
	}

	scanners := make([]*bufio.Scanner, len(files))
	filenames := make([]string, len(files))

	// Open all files and create scanners
	for i, file := range files {
		f, err := os.Open(file)
		if err != nil {
			fmt.Printf("Error opening file %s: %s\n", file, err)
			continue
		}
		defer f.Close()
		scanners[i] = bufio.NewScanner(f)
		filenames[i] = filepath.Base(file)
	}

	timestamps := make([]time.Time, len(files))
	restOfLines := make([]string, len(files))
	errors := make([]error, len(files))

	// Read the first timestamp from each file
	for i := range scanners {
		timestamps[i], restOfLines[i], errors[i] = readNextTimestamp(scanners[i])
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

		// Print the earliest timestamp with the filename prefix and the rest of the line
		filenamePrefix := getFilenamePrefix(filenames[earliestIndex])
		fmt.Printf("%s: %s: %s\n", filenamePrefix, earliestTime.Format("2006-01-02 15:04:05"), restOfLines[earliestIndex])

		// Read the next timestamp from the file that had the earliest timestamp
		timestamps[earliestIndex], restOfLines[earliestIndex], errors[earliestIndex] = readNextTimestamp(scanners[earliestIndex])
	}
}
