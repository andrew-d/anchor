// Pure utility functions — no Preact dependency.
// Shared by app.js (browser) and app_test.js (Node.js).

export function formatTime(unixSeconds) {
    const d = new Date(unixSeconds * 1000);
    return { local: d.toLocaleString(), utc: d.toUTCString() };
}

export function formatRelativeTime(unixSeconds) {
    const seconds = Math.floor(Date.now() / 1000 - unixSeconds);
    if (seconds < 0) return 'just now';
    if (seconds < 10) return 'just now';
    if (seconds < 60) return `${seconds}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}h ago`;
    return `${Math.floor(seconds / 86400)}d ago`;
}

export function formatDuration(seconds) {
    if (seconds < 60) return `${seconds} seconds`;
    if (seconds < 3600) {
        const m = Math.floor(seconds / 60);
        return m === 1 ? '1 minute' : `${m} minutes`;
    }
    const h = Math.floor(seconds / 3600);
    return h === 1 ? '1 hour' : `${h} hours`;
}

export function formatTimeAgo(timestampMs) {
    const seconds = Math.floor((Date.now() - timestampMs) / 1000);
    if (seconds < 10) return 'just now';
    if (seconds < 60) return `${seconds}s ago`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ago`;
    return `${Math.floor(seconds / 3600)}h ago`;
}

export function createConnectionTracker() {
    const tracker = {
        status: 'connected',
        lastSuccess: Date.now(),
        failures: 0,
        listeners: new Set(),

        success() {
            this.failures = 0;
            this.lastSuccess = Date.now();
            this._update();
        },

        failure() {
            this.failures++;
            this._update();
        },

        _update() {
            const prev = this.status;
            if (this.failures === 0) this.status = 'connected';
            else if (this.failures < 3) this.status = 'degraded';
            else this.status = 'disconnected';
            if (this.status !== prev) {
                this.listeners.forEach(fn => fn());
            }
        }
    };
    return tracker;
}
