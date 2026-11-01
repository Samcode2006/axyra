import { test } from 'node:test';
import assert from 'node:assert/strict';
import { tenantTrendLines, tenantTrendOptions } from '../src/features/analytics/panels/tenantTrends.ts';

const tenants = [{ id: 'a', name: 'Store A' }, { id: 'b', name: 'Store B' }, { id: 'empty', name: 'Empty store' }];
const points = [
  { tenant_id: 'a', bucket: '2026-11-01T00:00:00Z', order_count: 10, revenue: 100 },
  { tenant_id: 'b', bucket: '2026-11-01T00:00:00Z', order_count: 20, revenue: 200 },
  { tenant_id: 'a', bucket: '2026-11-02T00:00:00Z', order_count: 5, revenue: 50 },
];

test('tenants have separate lines, distinct colors, aligned buckets and exact values', () => {
  const options = tenantTrendOptions(tenants, points);
  const lines = tenantTrendLines(options, points, 'order_count', '');
  assert.deepEqual(lines.map((line) => line.name), ['Store A', 'Store B']);
  assert.notEqual(lines[0].color, lines[1].color);
  assert.deepEqual(lines.map((line) => line.points.map((point) => point.value)), [[10, 5], [20, 0]]);
});

test('selecting a tenant keeps its color and switches metric without mixing tenants', () => {
  const options = tenantTrendOptions(tenants, points);
  const all = tenantTrendLines(options, points, 'order_count', '');
  const selected = tenantTrendLines(options, points, 'revenue', 'b');
  assert.equal(selected.length, 1);
  assert.equal(selected[0].color, all[1].color);
  assert.deepEqual(selected[0].points.map((point) => point.value), [200, 0]);
  assert.deepEqual(tenantTrendLines(options, points, 'revenue', 'empty'), []);
});

test('a tenant outside the paginated names list remains selectable', () => {
  const options = tenantTrendOptions([], points);
  assert.deepEqual(options.map((option) => option.id), ['a', 'b']);
  assert.equal(tenantTrendLines(options, points, 'order_count', '').length, 2);
});
