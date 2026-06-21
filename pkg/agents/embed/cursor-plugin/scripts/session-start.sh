#!/bin/bash
# repocache:managed — sessionStart hook: refresh the cache in the background
# and inject the repocache guide into the conversation's initial system context.
repocache __bg-sync >/dev/null 2>&1 &
exec repocache __session-context --cursor
