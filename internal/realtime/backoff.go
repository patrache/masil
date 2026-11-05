package realtime

// Reconnect backoff: delay = rand(0, min(30s, 0.5s × 2^n)), n reset after 60s live.
// TODO(core): Task 13
