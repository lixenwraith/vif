#!/usr/bin/env bash

# --- Script Start ---
# Strict Mode
set -u
set -o pipefail

# --- Configuration ---
OUTPUT_FILE=""
DEFAULT_OUTPUT_FILE="./combined.txt"
RECURSIVE=false
DEBUG=false
REMOVE_COMMENTS=false
INPUT_LIST_FILE=""

# Arrays for multiple values
declare -a SOURCE_DIRS=()
declare -a INCLUDE_PATTERNS=()
declare -a EXCLUDE_PATTERNS=()

# --- Colors ---
if [[ -t 2 ]]; then
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[0;33m'
  BLUE='\033[0;34m'
  CYAN='\033[0;36m'
  WHITE='\033[1;37m'
  NC='\033[0m' # No Color
else
  RED=''
  GREEN=''
  YELLOW=''
  BLUE=''
  CYAN=''
  WHITE=''
  NC=''
fi

# --- Global Counters ---
file_count=0
total_lines=0
total_bytes=0
skipped_count=0

# --- Helper Functions ---

log_debug() {
  if [[ "$DEBUG" == true ]]; then
    echo -e "${YELLOW}[DEBUG]${NC} $1" >&2
  fi
}

format_size() {
  local size_bytes=$1
  awk -v size="$size_bytes" 'BEGIN { printf "%.2f KB", size/1024 }'
}

usage() {
  cat << EOF >&2
Usage: $0 [options] [[-s] <dir1>] ...

Combines multiple text files into a single output file with headers.
Smartly handles specific file paths and wildcard patterns.

Options:
  -o <file>      Path for the output file (default: combined.txt).
  
  -i <pattern>   Include filename pattern or specific path.
                 Examples: '*.go', 'cmd/main.go', 'utils/*.js'.
                 Can be used multiple times.
                 
  -f <file>      Read a list of patterns/filenames from a file (one per line).
                 Lines starting with '#' are ignored.
                 
  -e <pattern>   Exclude filename pattern (e.g., '*_test.go').
  
  -s <dir>       Add a source directory to scan. 
                 (Positional arguments are also treated as source directories).
                 
  -r             Enable recursive search in directories.
  
  -c             Remove full-line comments (requires 'rg'). 
                 Supports: '.go' (//) and '.sh/.bash' (#).
                 
  -d, --debug    Enable verbose debug output.
  
  -h, --help     Display this help message.

Example:
  $0 -r -o full_app.txt -s ./ -f file_list.txt -e '*_test.go' -c
EOF
  exit 1
}

# --- Argument Parsing ---
while [[ $# -gt 0 ]]; do
  key="$1"
  case $key in
    -o)
      OUTPUT_FILE="$2"; shift 2 ;;
    -i)
      INCLUDE_PATTERNS+=("$2"); shift 2 ;;
    -f)
      INPUT_LIST_FILE="$2"; shift 2 ;;
    -e)
      EXCLUDE_PATTERNS+=("$2"); shift 2 ;;
    -s)
      SOURCE_DIRS+=("$2"); shift 2 ;;
    -r)
      RECURSIVE=true; shift ;;
    -d|--debug)
      DEBUG=true; shift ;;
    -c)
      REMOVE_COMMENTS=true; shift ;;
    -h|--help)
      usage ;;
    *)
      if [[ "$1" == -* ]]; then 
        echo -e "${RED}Error:${NC} Unknown option '$1'." >&2; usage
      fi
      SOURCE_DIRS+=("$1"); shift ;;
  esac
done

# --- Process Input File (-f) ---
if [[ -n "$INPUT_LIST_FILE" ]]; then
  if [[ ! -f "$INPUT_LIST_FILE" ]]; then
    echo -e "${RED}Error:${NC} Input list file '$INPUT_LIST_FILE' not found." >&2
    exit 1
  fi
  log_debug "Reading patterns from: $INPUT_LIST_FILE"
  while IFS= read -r line || [[ -n "$line" ]]; do
    # Trim whitespace
    line=$(echo "$line" | xargs)
    # Skip empty lines and comments
    if [[ -z "$line" || "$line" == \#* ]]; then
      continue
    fi
    INCLUDE_PATTERNS+=("$line")
  done < "$INPUT_LIST_FILE"
fi

# --- Validation ---
if [[ ${#SOURCE_DIRS[@]} -eq 0 ]]; then
  # Default to current directory if no source provided
  SOURCE_DIRS+=(".")
  log_debug "No source dir specified, defaulting to current directory (.)"
fi

if [[ "$REMOVE_COMMENTS" == true ]]; then
  if ! command -v rg &> /dev/null; then
    echo -e "${RED}Error:${NC} '-c' flag requires 'ripgrep' (rg) to be installed." >&2
    exit 1
  fi
fi

log_debug "Config: Recursive=$RECURSIVE, Clean=$REMOVE_COMMENTS"
log_debug "Sources: ${SOURCE_DIRS[*]}"
log_debug "Includes: ${INCLUDE_PATTERNS[*]}"
log_debug "Excludes: ${EXCLUDE_PATTERNS[*]}"

# --- Core Logic ---

combine_files() {
  log_debug "Starting file combination process..."
  
  local first_file_processed=true
  local find_cmd_base="find"

  for dir in "${SOURCE_DIRS[@]}"; do
    log_debug "Scanning directory: '$dir'"
    if [[ ! -d "$dir" ]]; then
      echo -e "${YELLOW}Warning:${NC} '$dir' is not a valid directory. Skipping." >&2
      continue
    fi

    # Build Find Command Args
    local find_args=("$dir")
    
    if [[ "$RECURSIVE" == false ]]; then 
      find_args+=("-maxdepth" "1")
    fi
    
    find_args+=("-type" "f")

    # Handle Includes (OR logic) with Smart Path Detection
    if [[ ${#INCLUDE_PATTERNS[@]} -gt 0 ]]; then
      find_args+=("(")
      for i in "${!INCLUDE_PATTERNS[@]}"; do
        local pat="${INCLUDE_PATTERNS[$i]}"
        
        # Check if pattern contains a path separator
        if [[ "$pat" == *"/"* ]]; then
          # Use -path (wholename). 
          # We prepend '*' to match relative paths (e.g. './cmd/foo' matching 'cmd/foo')
          # unless the user explicitly started with ./ or /
          if [[ "$pat" == ./* || "$pat" == /* ]]; then
             find_args+=("-path" "$pat")
          else
             find_args+=("-path" "*$pat")
          fi
        else
          # No separator, assume just filename
          find_args+=("-name" "$pat")
        fi

        # Add OR operator if not the last item
        if [[ $i -lt $((${#INCLUDE_PATTERNS[@]} - 1)) ]]; then
          find_args+=("-o")
        fi
      done
      find_args+=(")")
    fi

    # Handle Excludes (AND logic)
    if [[ ${#EXCLUDE_PATTERNS[@]} -gt 0 ]]; then
      for pattern in "${EXCLUDE_PATTERNS[@]}"; do
        # Use ! instead of -not for BSD/macOS compatibility
        # Check for path separator in excludes too
        if [[ "$pattern" == *"/"* ]]; then
           find_args+=("!" "-path" "*$pattern")
        else
           find_args+=("!" "-name" "$pattern")
        fi
      done
    fi

    # Print with null terminator for safety
    find_args+=("-print0")
    
    log_debug "Find command args: ${find_args[*]}"

    # Execute find and read stream
    while IFS= read -r -d $'\0' file_path || [[ -n "$file_path" ]]; do
      
      # 1. Check Readability
      if [[ ! -r "$file_path" ]]; then
          log_debug "Cannot read '$file_path'. Skipping."
          ((skipped_count++))
          continue
      fi

      # 2. Calculate Stats
      local current_lines
      local current_bytes
      current_lines=$(wc -l < "$file_path" || echo 0)
      current_bytes=$(wc -c < "$file_path" || echo 0)
      local formatted_size
      formatted_size=$(format_size "$current_bytes")

      # 3. Prepare Display Info
      local filename
      local dirname
      filename=$(basename "$file_path")
      dirname=$(dirname "$file_path")
      
      # Print formatted file entry to STDERR (Console)
      printf "${RED}%s/${NC}${WHITE}%s${NC} ${GREEN}[%s lines]${NC} ${BLUE}[%s]${NC}\n" "$dirname" "$filename" "$current_lines" "$formatted_size" >&2

      # 4. Handle Separator
      if [[ "$first_file_processed" = false ]]; then
        echo "" # Newline separator in output file
      fi
      first_file_processed=false

      # 5. Output Header
      local rel_path="${file_path#./}"
      echo "// FILE: $rel_path"

      # 6. Output Content
      if [[ "$REMOVE_COMMENTS" == true ]]; then
        if [[ "$filename" == *.go ]]; then
          # Remove Go comments //
          rg -v '^\s*//' "$file_path"
        elif [[ "$filename" == *.sh || "$filename" == *.bash || "$filename" == *.py || "$filename" == *.yml || "$filename" == *.yaml ]]; then
          # Remove Shell/Python/Yaml comments #
          rg -v '^\s*#' "$file_path"
        else
          cat "$file_path"
        fi
      else
        cat "$file_path"
      fi

      # 7. Update Globals
      ((file_count++))
      total_lines=$((total_lines + current_lines))
      total_bytes=$((total_bytes + current_bytes))

    done < <( "$find_cmd_base" "${find_args[@]}" )

  done
}

# --- Execution ---

FINAL_OUTPUT_FILE="${OUTPUT_FILE:-$DEFAULT_OUTPUT_FILE}"
OUTPUT_DIR=$(dirname "$FINAL_OUTPUT_FILE")
if [[ ! -d "$OUTPUT_DIR" ]]; then
  mkdir -p "$OUTPUT_DIR"
fi

log_debug "Outputting to: $FINAL_OUTPUT_FILE"

# Run Combination
combine_files > "$FINAL_OUTPUT_FILE"
combine_status=$?

# --- Final Summary ---
final_size_bytes=0
if [[ -f "$FINAL_OUTPUT_FILE" ]]; then
  final_size_bytes=$(wc -c < "$FINAL_OUTPUT_FILE")
fi
formatted_total_size=$(format_size "$final_size_bytes")

echo -e "----------------------------------------" >&2
if [[ $combine_status -eq 0 && $file_count -gt 0 ]]; then
  echo -e "${CYAN}Combination Complete${NC}" >&2
  echo -e "Files Processed: ${WHITE}$file_count${NC}" >&2
  echo -e "Total Source Lines: ${GREEN}$total_lines${NC}" >&2
  echo -e "Output File Size: ${BLUE}$formatted_total_size${NC}" >&2
  echo -e "Saved to: ${YELLOW}$FINAL_OUTPUT_FILE${NC}" >&2
else
  echo -e "${RED}Combination Failed or No Files Found${NC}" >&2
  if [[ "$skipped_count" -gt 0 ]]; then echo -e "Skipped Files: $skipped_count" >&2; fi
  exit 1
fi
echo -e "----------------------------------------" >&2

exit 0
