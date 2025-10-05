package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/100days/logmerge/mergelogs"
)

func main() {
	// Define command-line flags for start and end times
	startTimeStr := flag.String("start", "", "Start time (format: 2006-01-02T15:04:05)")
	endTimeStr := flag.String("end", "", "End time (format: 2006-01-02T15:04:05)")
	fieldSeparator := flag.String("sep", " ", "Field separator")
	verbose := flag.Bool("v", false, "Verbose output")
	flag.Parse()

	// Get the remaining arguments (file patterns)
	files := flag.Args()
	if len(files) == 0 {
		_, _ = flag.CommandLine.Output().Write([]byte("No files specified\nUsage: logmerge [switches] <file1> <file2> ... <fileN>\nSwitches:\n"))
		flag.PrintDefaults()
		os.Exit(1)
	}

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

	// Create options for the merge operation
	opts := mergelogs.Options{
		Files:          files,
		StartTime:      startTime,
		EndTime:        endTime,
		FieldSeparator: *fieldSeparator,
		Verbose:        *verbose,
	}

	// Create buffered writer for output
	outputWriter := bufio.NewWriter(os.Stdout)

	// Process the logs
	stats, err := mergelogs.ProcessLogs(opts, outputWriter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error processing logs: %v\n", err)
		os.Exit(1)
	}

	// Print statistics if verbose
	if *verbose {
		fmt.Fprintf(os.Stderr, "Lines: %d\n", stats.ProcessedLines)
		fmt.Fprintf(os.Stderr, "Cache hits: %d\n", stats.CacheHits)
		fmt.Fprintf(os.Stderr, "Duration %s\n", stats.Duration)
	}
}
