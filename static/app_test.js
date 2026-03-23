import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import {
    formatTime, formatRelativeTime, formatDuration, formatTimeAgo,
    createConnectionTracker,
} from './util.js';

describe('formatTime', () => {
    it('returns local and utc strings', () => {
        const result = formatTime(1700000000);
        assert.ok(typeof result.local === 'string');
        assert.ok(typeof result.utc === 'string');
        assert.ok(result.utc.includes('2023'));
    });

    it('handles epoch zero', () => {
        const result = formatTime(0);
        assert.ok(result.utc.includes('1970'));
    });
});

describe('formatRelativeTime', () => {
    it('returns "just now" for recent timestamps', () => {
        const now = Math.floor(Date.now() / 1000);
        assert.equal(formatRelativeTime(now), 'just now');
        assert.equal(formatRelativeTime(now - 5), 'just now');
    });

    it('returns seconds for <60s', () => {
        const now = Math.floor(Date.now() / 1000);
        assert.equal(formatRelativeTime(now - 30), '30s ago');
    });

    it('returns minutes for <1h', () => {
        const now = Math.floor(Date.now() / 1000);
        assert.equal(formatRelativeTime(now - 300), '5m ago');
    });

    it('returns hours for <24h', () => {
        const now = Math.floor(Date.now() / 1000);
        assert.equal(formatRelativeTime(now - 7200), '2h ago');
    });

    it('returns days for >=24h', () => {
        const now = Math.floor(Date.now() / 1000);
        assert.equal(formatRelativeTime(now - 172800), '2d ago');
    });

    it('returns "just now" for future timestamps', () => {
        const future = Math.floor(Date.now() / 1000) + 100;
        assert.equal(formatRelativeTime(future), 'just now');
    });
});

describe('formatDuration', () => {
    it('formats seconds', () => {
        assert.equal(formatDuration(30), '30 seconds');
        assert.equal(formatDuration(1), '1 seconds');
    });

    it('formats minutes', () => {
        assert.equal(formatDuration(60), '1 minute');
        assert.equal(formatDuration(120), '2 minutes');
        assert.equal(formatDuration(300), '5 minutes');
    });

    it('formats hours', () => {
        assert.equal(formatDuration(3600), '1 hour');
        assert.equal(formatDuration(7200), '2 hours');
    });
});

describe('formatTimeAgo', () => {
    it('returns "just now" for recent timestamps', () => {
        assert.equal(formatTimeAgo(Date.now()), 'just now');
        assert.equal(formatTimeAgo(Date.now() - 5000), 'just now');
    });

    it('returns seconds for <60s', () => {
        assert.equal(formatTimeAgo(Date.now() - 30000), '30s ago');
    });

    it('returns minutes for <1h', () => {
        assert.equal(formatTimeAgo(Date.now() - 300000), '5m ago');
    });

    it('returns hours for >=1h', () => {
        assert.equal(formatTimeAgo(Date.now() - 7200000), '2h ago');
    });
});

describe('createConnectionTracker', () => {
    it('starts as connected', () => {
        const conn = createConnectionTracker();
        assert.equal(conn.status, 'connected');
        assert.equal(conn.failures, 0);
    });

    it('stays connected after success', () => {
        const conn = createConnectionTracker();
        conn.success();
        assert.equal(conn.status, 'connected');
    });

    it('becomes degraded after 1-2 failures', () => {
        const conn = createConnectionTracker();
        conn.failure();
        assert.equal(conn.status, 'degraded');
        conn.failure();
        assert.equal(conn.status, 'degraded');
    });

    it('becomes disconnected after 3+ failures', () => {
        const conn = createConnectionTracker();
        conn.failure();
        conn.failure();
        conn.failure();
        assert.equal(conn.status, 'disconnected');
    });

    it('resets to connected on success after failures', () => {
        const conn = createConnectionTracker();
        conn.failure();
        conn.failure();
        conn.failure();
        assert.equal(conn.status, 'disconnected');
        conn.success();
        assert.equal(conn.status, 'connected');
        assert.equal(conn.failures, 0);
    });

    it('notifies listeners on status change', () => {
        const conn = createConnectionTracker();
        const changes = [];
        conn.listeners.add(() => changes.push(conn.status));

        conn.failure(); // connected -> degraded
        assert.deepEqual(changes, ['degraded']);

        conn.failure(); // still degraded, no notification
        assert.deepEqual(changes, ['degraded']);

        conn.failure(); // degraded -> disconnected
        assert.deepEqual(changes, ['degraded', 'disconnected']);

        conn.success(); // disconnected -> connected
        assert.deepEqual(changes, ['degraded', 'disconnected', 'connected']);
    });
});
