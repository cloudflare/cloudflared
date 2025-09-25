#!/bin/bash
set -e

# Define the file to store the list of vulnerabilities to ignore.
IGNORE_FILE=".vulnignore"

# Check if the ignored vulnerabilities file exists. If not, create an empty one.
if [ ! -f "$IGNORE_FILE" ]; then
    touch "$IGNORE_FILE"
    echo "Created an empty file to store ignored vulnerabilities: $IGNORE_FILE"
    echo "# Add vulnerability IDs (e.g., GO-2022-0450) to ignore, one per line." >> "$IGNORE_FILE"
    echo "# You can also add comments on the same line after the ID." >> "$IGNORE_FILE"
    echo "" >> "$IGNORE_FILE"
fi

# Run govulncheck and capture its output.
VULN_OUTPUT=$(go run -mod=readonly golang.org/x/vuln/cmd/govulncheck@latest ./... || true)

# Print the govuln output
echo "====================================="
echo "Full Output of govulncheck:"
echo "====================================="
echo "$VULN_OUTPUT"
echo "====================================="
echo "End of govulncheck Output"
echo "====================================="

# Process the ignore file to remove comments and empty lines.
# The 'cut' command gets the vulnerability ID and removes anything after the '#'.
# The 'grep' command filters out empty lines and lines starting with '#'.
CLEAN_IGNORES=$(grep -v '^\s*#' "$IGNORE_FILE" | cut -d'#' -f1 | sed 's/ //g' | sort -u || true)

# Filter out the ignored vulnerabilities.
UNIGNORED_VULNS=$(echo "$VULN_OUTPUT" | grep 'Vulnerability')

# If the list of ignored vulnerabilities is not empty, filter them out.
if [ -n "$CLEAN_IGNORES" ]; then
    UNIGNORED_VULNS=$(echo "$UNIGNORED_VULNS" | grep -vFf <(echo "$CLEAN_IGNORES") || true)
fi

# If there are any vulnerabilities that were not in our ignore list, print them and exit with an error.
if [ -n "$UNIGNORED_VULNS" ]; then
    echo "🚨 Found new, unignored vulnerabilities:"
    echo "-------------------------------------"
    echo "$UNIGNORED_VULNS"
    echo "-------------------------------------"
    echo "Exiting with an error. ❌"
    exit 1
else
    echo "🎉 No new vulnerabilities found. All clear! ✨"
    exit 0
fi
