import { manifest } from './manifest';

test('plugin manifest, id and version are defined', () => {
  expect(manifest).toBeDefined();
  expect(manifest.id).toBeDefined();
  expect(manifest.version).toBeDefined();
});

// To ease migration, verify separate export of id and version.
test('plugin id and version are defined', () => {
  expect(manifest.id).toBeDefined();
  expect(manifest.version).toBeDefined();
});
