#!/usr/bin/env bash
# Start the bot (single-instance guard via PID file)
set -e

PIDFILE="bot.pid"
LOGFILE="bot.log"
BINARY="./polymarket-bot"

if [ -f "$PIDFILE" ]; then
    PID=$(cat "$PIDFILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Bot already running (PID $PID)"
        exit 0
    else
        echo "Stale PID file, removing..."
        rm -f "$PIDFILE"
    fi
fi

if [ ! -f "$BINARY" ]; then
    echo "Binary not found — building..."
    go build -o polymarket-bot ./cmd/bot
fi

echo "Starting bot..."
nohup "$BINARY" >> "$LOGFILE" 2>&1 &
echo $! > "$PIDFILE"
echo "Bot started (PID $(cat $PIDFILE)) — logs: $LOGFILE"
