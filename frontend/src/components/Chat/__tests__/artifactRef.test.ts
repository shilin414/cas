/**
 * ArtifactMarkdown 嵌套路径匹配回归测试。
 *
 * 回归背景（P0-6）：智能体一次返回两张图片时，第一张显示、第二张不显示。
 *
 * 根因是 `resolveArtifactSrc` 的正则：
 *   /^artifacts?\/([^/]+)(?:\/.*)?$/i
 * 它只捕获**第一个路径段**，而 Aily 实际发出的引用形如
 *   artifacts/<name>/<...nested...>/<file>
 * 于是任何带嵌套目录的引用都会丢掉名字 → 匹配不到 artifact → 渲染成
 * 「生成产物」占位符，`<img>` 永远不会出现。扁平路径（第一张图常见形态）
 * 恰好能命中，所以症状表现为「只有第二张不显示」。
 *
 * 本测试直接断言匹配函数本身：一维与嵌套引用都必须解析到同一个
 * artifactId。
 */
import { describe, expect, it } from 'vitest';

import { resolveArtifactRef } from '../artifactRef';

const ARTIFACTS = [
  { artifactId: 'art-flat', name: 'chart.png' },
  { artifactId: 'art-nested', name: 'report.png' },
  { artifactId: 'art-subdir', name: 'sub/dir/photo.jpg' },
  { artifactId: 'art-named', name: 'output' },
];

const resolve = (src: string) =>
  resolveArtifactRef(src, ARTIFACTS, (id) => `/open/${id}`);

describe('resolveArtifactRef', () => {
  it('resolves a flat artifacts/<name>/<file> ref', () => {
    expect(resolve('artifacts/chart.png/chart.png')).toBe('/open/art-flat');
  });

  it('resolves a ref with NO trailing filename', () => {
    expect(resolve('artifacts/chart.png')).toBe('/open/art-flat');
  });

  // The bug: the old regex captured only the first segment, so a nested path
  // lost its name and produced no match at all.
  it('resolves a multi-segment nested ref', () => {
    expect(resolve('artifacts/report.png/2026/09/report.png')).toBe('/open/art-nested');
  });

  it('resolves a deeply nested ref', () => {
    expect(resolve('artifacts/report.png/a/b/c/d/e/report.png')).toBe('/open/art-nested');
  });

  it('resolves by trailing filename when the first segment differs', () => {
    expect(resolve('artifacts/generated/uuid-1234/photo.jpg')).toBe('/open/art-subdir');
  });

  it('matches a name whose directory part is nested', () => {
    expect(resolve('artifacts/photo.jpg/x/photo.jpg')).toBe('/open/art-subdir');
  });

  it('matches after dropping an extension the ref omitted', () => {
    expect(resolve('artifacts/output/x/y/output')).toBe('/open/art-named');
  });

  it('tolerates ./ and leading slash prefixes', () => {
    expect(resolve('./artifacts/chart.png/c.png')).toBe('/open/art-flat');
    expect(resolve('/artifacts/chart.png/c.png')).toBe('/open/art-flat');
  });

  it('tolerates query and fragment suffixes', () => {
    expect(resolve('artifacts/report.png/a/b/report.png?x=1')).toBe('/open/art-nested');
    expect(resolve('artifacts/report.png/a/b/report.png#frag')).toBe('/open/art-nested');
  });

  it('tolerates a bare filename that equals an artifact name', () => {
    expect(resolve('chart.png')).toBe('/open/art-flat');
  });

  it('returns null for an unknown ref so the caller shows a placeholder', () => {
    expect(resolve('artifacts/nope.png/a/nope.png')).toBeNull();
  });
});

const galleryRefs = [
  'artifacts/landscape_gallery/snow_mountain_lake.jpg',
  'artifacts/landscape_gallery/forest_sunset.jpg',
];
describe('multi-file artifact identity', () => {
  it('keeps gallery filenames on separate resolver requests', () => {
    const artifacts = [{artifactId:'gallery', name:'landscape_gallery'}];
    expect(resolveArtifactRef(galleryRefs[0], artifacts, id => `/open/${id}`, galleryRefs))
      .toBe('/open/gallery?filename=snow_mountain_lake.jpg');
    expect(resolveArtifactRef(galleryRefs[1], artifacts, id => `/open/${id}`, galleryRefs))
      .toBe('/open/gallery?filename=forest_sunset.jpg');
  });
  it('handles historical snapshots that incorrectly named a gallery after its first file', () => {
    const artifacts = [{artifactId:'gallery',name:'snow_mountain_lake.jpg'}];
    expect(resolveArtifactRef(galleryRefs[1], artifacts, id=>`/open/${id}`, galleryRefs))
      .toBe('/open/gallery?filename=forest_sunset.jpg');
  });
  it('prefers the actual filename over a shared directory label', () => {
    const artifacts = [{artifactId:'directory',name:'landscape_gallery'},{artifactId:'forest',name:'forest_sunset.jpg'}];
    expect(resolveArtifactRef(galleryRefs[1],artifacts,id=>`/open/${id}`)).toBe('/open/forest');
  });
  it('never uses a shared prefix as proof of file identity', () => {
    expect(resolveArtifactRef('artifacts/chart/other.png',[{artifactId:'wrong',name:'chart-other.png'}],id=>id)).toBeNull();
  });
  it('does not crash on malformed percent escapes', () => {
    expect(resolveArtifactRef('artifacts/%oops/file.png',[],id=>id)).toBeNull();
  });
});

describe('artifact reference validation', () => {
  it.each(Array.from({ length: 32 }, (_, code) => code))('rejects raw and encoded control character %i in every path position', code => {
    const control = String.fromCharCode(code);
    const encoded = `%${code.toString(16).padStart(2, '0')}`;
    for (const value of [control, encoded]) {
      for (const src of [
        `artifacts/bad${value}/chart.png`,
        `artifacts/chart.png/bad${value}/chart.png`,
        `artifacts/chart.png/bad${value}.png`,
      ]) expect(resolve(src)).toBeNull();
    }
  });

  it.each([
    '..', '%2e%2e', '%2E%2e', '.%2e', '%2e.', '%2f', '%2F', '%5c', '%5C',
    '..%2fother', '..%5cother', '\\', '%', '%2', '%GG', '%C0%AF',
  ])('rejects traversal, encoded separators and malformed encoding: %s', segment => {
    expect(resolve(`artifacts/${segment}/chart.png`)).toBeNull();
    expect(resolve(`artifacts/chart.png/${segment}/chart.png`)).toBeNull();
    expect(resolve(`artifacts/chart.png/${segment}`)).toBeNull();
  });

  it('preserves safe percent-encoded Unicode, spaces and dot-containing names', () => {
    const name = '报表 chart.v2.png';
    expect(resolveArtifactRef(`artifacts/output/${encodeURIComponent(name)}`,
      [{ artifactId: 'safe', name }], id => `/open/${id}`)).toBe('/open/safe');
    expect(resolve('artifacts/output/report%2epng')).toBe('/open/art-nested');
  });
});
