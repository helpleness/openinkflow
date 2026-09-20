#!/bin/bash

set -e

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_DIR="$DIR/log"
LOG_FILE="$DIR/nohup.out"
PID_FILE="$LOG_DIR/inkflow.pid"

export LD_LIBRARY_PATH="$DIR/lib:$LD_LIBRARY_PATH"

mkdir -p "$LOG_DIR"

if [ -f "$PID_FILE" ]; then
  OLD_PID="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
    echo "InkFlow is already running, pid: $OLD_PID"
    echo "Log file: $LOG_FILE"
    exit 0
  fi
fi

echo "Starting InkFlow in background..."
nohup "$DIR/InkFlow" "$@" >>"$LOG_FILE" 2>&1 &
PID="$!"
echo "$PID" >"$PID_FILE"

echo "InkFlow started, pid: $PID"
echo "Log file: $LOG_FILE"
echo "View logs: tail -f \"$LOG_FILE\""
