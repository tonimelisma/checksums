# checksumtool

checksumtool is a command-line utility for calculating and comparing file checksums. It allows you to efficiently manage checksums for a large number of files and detect any changes or mismatches.

My personal use case is to detect bit rot in any pictures. Throughout the years old photos get corrupted. This utility detects corrupted photos, allowing me to restore them from backups.

## Features

- Calculate checksums for files in one or more directories
- Compare checksums against a stored database to detect changes or mismatches
- Update the checksum database with new or changed files
- List files that are missing from the checksum database
- Add checksums for missing files to the database
- Progress tracking and estimation of remaining time
- Interrupt handling to save work done so far

## Usage
checksumtool [flags] [directories...]

### Flags
- `-db string`: Checksum database file location (default "$HOME/.local/lib/checksums.json")
- `-verbose`: Enable verbose output
- `-mode string`: Operation mode: check, update, list-missing, add-missing
- `-workers int`: Number of worker goroutines (default 4)

### Operation Modes
- `check`: Compare checksums of files against the stored database and report any mismatches.
- `update`: Update the checksum database with new or changed files.
- `list-missing`: List files that are missing from the checksum database.
- `add-missing`: Add checksums for missing files to the database.

### Examples

`checksumtool -mode check ~/Documents ~/Pictures`

Compare checksums of files in the "Documents" and "Pictures" directories against the stored database.

`checksumtool -mode update -verbose ~/Projects`

Update the checksum database with files from the "Projects" directory and enable verbose output.

`checksumtool -mode list-missing ~/Music`

List files in the "Music" directory that are missing from the checksum database.

`checksumtool -mode add-missing -workers 8 ~/Videos`

Add checksums for missing files in the "Videos" directory to the database, using 8 worker goroutines.

## Attribution

checksumtool is developed by Toni Melisma and released in 2024. The code was almost entirely written by Claude 3 Opus based on my algorithm and instructions.