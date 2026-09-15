import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fmtDuration, fmtRelative, secondsLeft, fmtExpiry, fmtBytes, ttlSeconds, fmtGeo, fmtDecider, fmtTime } from '../js/format.js';

const T0 = Date.parse('2026-09-15T10:00:00Z');

test('fmtDuration picks the unit', () => {
  assert.equal(fmtDuration(0), '0 s');
  assert.equal(fmtDuration(45), '45 s');
  assert.equal(fmtDuration(180), '3 min');
  assert.equal(fmtDuration(3600), '1 h');
  assert.equal(fmtDuration(5400), '1.5 h');
  assert.equal(fmtDuration(86400 * 3), '3 d');
});

test('fmtRelative says ago or in', () => {
  assert.equal(fmtRelative('2026-09-15T09:57:00Z', T0), '3 min ago');
  assert.equal(fmtRelative('2026-09-15T11:00:00Z', T0), 'in 1 h');
  assert.equal(fmtRelative('2026-09-15T10:00:00Z', T0), 'now');
  assert.equal(fmtRelative(null, T0), '');
});

test('secondsLeft never goes negative and rounds up', () => {
  assert.equal(secondsLeft('2026-09-15T10:00:30Z', T0), 30);
  assert.equal(secondsLeft('2026-09-15T10:00:00.5Z', T0), 1);
  assert.equal(secondsLeft('2026-09-15T09:59:00Z', T0), 0);
  assert.equal(secondsLeft('garbage', T0), 0);
});

test('fmtExpiry: null is permanent', () => {
  assert.equal(fmtExpiry(null), 'permanent');
  assert.match(fmtExpiry('2026-09-15T11:00:00Z', T0), /\(in 1 h\)$/);
});

test('fmtBytes', () => {
  assert.equal(fmtBytes(512), '512 B');
  assert.equal(fmtBytes(48213), '47.1 KB');
  assert.equal(fmtBytes(20 * 1024 * 1024), '20.0 MB');
  assert.equal(fmtBytes(null), '');
});

test('ttlSeconds maps the picker to the contract', () => {
  assert.equal(ttlSeconds('permanent'), null);
  assert.equal(ttlSeconds('1h'), 3600);
  assert.equal(ttlSeconds('24h'), 86400);
  assert.equal(ttlSeconds('custom', '7', 'd'), 7 * 86400);
  assert.equal(ttlSeconds('custom', '90', 'min'), 5400);
  assert.throws(() => ttlSeconds('custom', '0', 'h'), /positive/);
  assert.throws(() => ttlSeconds('custom', 'x', 'h'), /positive/);
  assert.throws(() => ttlSeconds('custom', '1', 'weeks'), /unit/);
  assert.throws(() => ttlSeconds('never'), /choice/);
});

test('fmtGeo tolerates missing keys', () => {
  assert.equal(fmtGeo(null), '');
  assert.equal(fmtGeo({ country: 'FR', city: 'Paris', asn: 'AS12876' }), 'Paris, FR (AS12876)');
  assert.equal(fmtGeo({ country: 'FR' }), 'FR');
  assert.equal(fmtGeo({ asn: 'AS1' }), 'AS1');
});

test('fmtDecider prefers the v1.1 admin name, then the device, then the rule', () => {
  assert.equal(fmtDecider({ decided_by: 'admin', decided_by_admin: 'alice', decided_by_device: 'Pixel 8' }), 'alice / Pixel 8');
  assert.equal(fmtDecider({ decided_by: 'admin', decided_by_device: 'Pixel 8' }), 'Pixel 8');
  assert.equal(fmtDecider({ decided_by: 'admin' }), 'admin');
  assert.equal(fmtDecider({ decided_by: 'timeout' }), 'nobody (timeout)');
  assert.equal(fmtDecider({ decided_by: 'autoblock' }), 'auto-block');
  assert.equal(fmtDecider({ decided_by: 'georule' }), 'geo rule');
  assert.equal(fmtDecider({ decided_by: 'whitelist' }), 'whitelist');
  assert.equal(fmtDecider({ decided_by: null }), '');
});

test('fmtTime is local and zero-padded, and passes garbage through', () => {
  assert.match(fmtTime('2026-09-15T10:00:00Z'), /^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);
  assert.equal(fmtTime(''), '');
  assert.equal(fmtTime('not a date'), 'not a date');
});
