import { test } from 'node:test';
import assert from 'node:assert/strict';
import { apiBase, query, errorMessage, ApiError } from '../js/api.js';

test('apiBase strips /dashboard and whatever follows, keeps a path prefix', () => {
  assert.equal(apiBase('/dashboard/'), '');
  assert.equal(apiBase('/dashboard'), '');
  assert.equal(apiBase('/dashboard/js/app.js'), '');
  assert.equal(apiBase('/api/dashboard/'), '/api');
  assert.equal(apiBase('/something-else'), '');
});

test('query skips empty values and encodes the rest', () => {
  assert.equal(query({}), '');
  assert.equal(query({ limit: 50, before: null, server: '', username: 'a b' }), '?limit=50&username=a+b');
  assert.equal(query({ status: 'pending', recorded: 'true' }), '?status=pending&recorded=true');
});

test('errorMessage shows the status for API errors and the text for the rest', () => {
  assert.equal(errorMessage(new ApiError(404, { error: 'not found' }, 'not found')), 'HTTP 404: not found');
  assert.equal(errorMessage(new ApiError(0, null, 'backend unreachable: x')), 'backend unreachable: x');
  assert.equal(errorMessage(new Error('boom')), 'boom');
  assert.equal(errorMessage('plain'), 'plain');
});
