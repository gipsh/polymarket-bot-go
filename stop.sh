#!/usr/bin/env bash
# Stop the bot
PIDFILE="bot.pid"

if [ ! -f "$PIDFILE" ]; then
    echo "No PID file — bot may not be running"
    exit 0
fi

PID=$(cat "$PIDFILE")
if kill -0 "$PID" 2>/dev/null; then
    kill "$PID"
    rm -f "$PIDFILE"
    echo "Bot stopped (PID $PID)"
else
    echo "Bot not running (stale PID $PID)"
    rm -f "$PIDFILE"
fi
