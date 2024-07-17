# logmerge

- reads logfiles
- tries to scan timestamp format in each line of each file
- merges all lines based on increasing timestamps

*NOTE: This assumes, timestamps are always increasing withing each logeilfe* 

usage:
```hell
logmerge -v -start 2024-07-16T10:23:43 -end 2024-07-16T20:34:22 /var/log/syslog /var/log/apache2/*.log
```

- -v: (optional) verbose output
- -start: (optional) start time: 2024-07-16T10:23:43
- -end: (optional) end time: (optional) 2024-07-16T20:34:22
- ARGS: (at least one required) files to read

Outputs on StdOut.

Each Output Line is in the format:

{{timestamp}}: {{filename}}: {{RemainingLine}}

- filename is the last 20 characters of the respective filename the log line came from
- timestamp is the timestamp in format: 2024-07-16 20:17:40
- RemainingLine is the log line minus timestamp
``
Currently, Lines without timestamps are ignored.
Next Step: include them in the previous logline with timestamp of this file so it is displayed in the right order