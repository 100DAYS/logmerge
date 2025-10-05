# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

`logmerge` is a Go command-line tool that merges multiple log files chronologically based on their timestamps. It reads log files, scans for timestamp formats in each line, and outputs merged logs sorted by increasing timestamps to stdout.

**Key assumption**: Timestamps are always increasing within each individual log file.

## Building and Testing

```bash
# Install dependencies
go get .

# Build for current platform
go build -o logmerge -v ./...

# Build for specific platforms
env GOOS=linux GOARCH=amd64 go build -o logmerge -v ./...
env GOOS=linux GOARCH=arm64 go build -o logmerge_arm -v ./...
env GOOS=windows GOARCH=amd64 go build -o logmerge.exe -v ./...

# Run tests
go test
```

## Usage

```bash
logmerge -v -start 2024-07-16T10:23:43 -end 2024-07-16T20:34:22 /var/log/syslog /var/log/apache2/*.log
```

**Flags:**
- `-v`: Verbose output (prints stats to stderr)
- `-start`: Optional start time filter (format: 2006-01-02T15:04:05)
- `-end`: Optional end time filter (format: 2006-01-02T15:04:05)
- `-sep`: Field separator (default: space)
- Arguments: At least one file or glob pattern required

**Output format:** `{{timestamp}} {{filename}} {{RemainingLine}}`
- Filename is truncated to last 20 characters
- Timestamp format: 2006-01-02 15:04:05
- RemainingLine is the log line with timestamp removed

## Architecture

### Core Components

**main.go** - Single-file implementation containing all logic:

1. **Timestamp Detection** (`timestampPatterns` at main.go:22-38)
   - Array of regex patterns and Go time layouts
   - Supports 11+ different log timestamp formats (syslog, ISO8601, Apache, strace, etc.)
   - Pattern matching uses a caching strategy per file for performance

2. **Pattern Matching Strategy** (`parseLogLine` at main.go:76)
   - First attempts cached pattern for the file (if known)
   - Falls back to `findBestMatch` which scans all patterns
   - Selects leftmost match, or longest match if same position
   - Cache hit ratio tracked in verbose mode

3. **Merge Algorithm** (`mergeLogs` at main.go:137)
   - Opens all files and creates scanners
   - Maintains current timestamp and line for each file
   - Repeatedly finds earliest timestamp across all files
   - Outputs line and advances that file's scanner
   - Uses channel to stream results to main goroutine
   - Handles lines without timestamps by keeping previous timestamp from that file

4. **Data Structures**
   - `lineStruct` (main.go:16): Holds timestamp, file index, and remaining line text
   - `logFormatIndexes` (main.go:60): Maps file index to cached pattern index
   - Per-file scanners and error tracking in `mergeLogs`

### Key Behaviors

- Lines without timestamps are currently assigned the previous timestamp from their file (not skipped)
- Year inference: If parsed timestamp has year 0, current year is added
- End time is incremented by 1 second for inclusive filtering
- File errors (open/read failures) are logged but don't stop processing other files
- Verbose mode outputs stats: processed lines, cache hits, duration

### Performance Optimizations

- Pattern caching: Once a pattern matches for a file, it's tried first for subsequent lines
- Regex pre-compilation: All patterns compiled at startup
- Single-pass merge: Each file read once in order
- Channel-based streaming: Outputs lines as they're merged (no buffering all in memory)