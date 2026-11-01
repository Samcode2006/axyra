import { test } from 'node:test';
import assert from 'node:assert/strict';
import { compactMetricValue, visibleMetrics, metricRows, metricValue } from '../src/features/analytics/panels/metricResults.ts';

test('new enabled organization metrics appear while disabled and foreign definitions stay hidden', () => {
  const definitions = [
    { id: 'base', name: 'Base', organization_id: null, enabled: true },
    { id: 'new_metric', name: 'New metric', organization_id: 'own', enabled: true },
    { id: 'disabled', name: 'Disabled', organization_id: 'own', enabled: false },
    { id: 'foreign', name: 'Foreign', organization_id: 'other', enabled: true },
  ];
  assert.deepEqual(visibleMetrics(definitions, 'own').map((m) => m.id), ['base', 'new_metric']);
  assert.deepEqual(visibleMetrics(definitions.slice(0, 1), 'own').map((m) => m.id), ['base']);
});

test('organization overrides win regardless of list order, including disabled overrides', () => {
  const builtin = { id: 'same', name: 'Base', organization_id: null, enabled: true };
  const override = { ...builtin, name: 'Override', organization_id: 'own' };
  assert.deepEqual(visibleMetrics([override, builtin], 'own'), [override]);
  assert.deepEqual(visibleMetrics([builtin, { ...override, enabled: false }], 'own'), []);
});

test('scalar zero, grouped results and empty results remain distinguishable', () => {
  assert.deepEqual(metricRows({ count: 0 }), [{ count: 0 }]);
  const grouped = [{ category: 'retail', coalesce: '1000.00' }];
  assert.deepEqual(metricRows(grouped), grouped);
  assert.deepEqual(metricRows(null), []);
  assert.equal(metricValue(0), '0');
  assert.equal(metricValue('9007199254740993.25'), '9007199254740993.25');
  assert.equal(metricValue(null), '—');
  assert.equal(compactMetricValue(10000), '10k');
  assert.equal(compactMetricValue('12500.00'), '12.5k');
  assert.equal(compactMetricValue('1200000.00'), '1.2M');
  assert.equal(compactMetricValue(-10000), '-10k');
  assert.equal(compactMetricValue(0), '0');
  assert.equal(compactMetricValue('12.50'), '12.50');
  assert.equal(compactMetricValue(null), '—');
  assert.equal(metricValue('10000'), '10000'); // Numeric-looking IDs and hover values stay exact.
});
