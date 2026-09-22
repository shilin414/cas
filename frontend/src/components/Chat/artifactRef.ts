/**
 * Artifact reference resolution (pure, testable).
 *
 * Aily embeds generated files as sandbox-relative refs of the shape
 *
 *	artifacts/<artifactName>/<...nested...>/<filename>
 *
 * which mean nothing outside the chat surface until they are mapped to a
 * local artifact id and rewritten to an /open resolver. This module owns
 * that mapping so it can be tested without rendering markdown.
 */

export interface ArtifactRefName {
  artifactId: string;
  name: string;
}

/**
 * Drops a trailing file extension, case-insensitively. Artifact refs and
 * artifact names disagree on whether the extension is present (the provider
 * keeps `name.png`, its sandbox ref may be `artifacts/<name>/…`), so
 * comparing the stems is what makes the two forms match.
 */
export function stripExt(name: string): string {
  const i = name.lastIndexOf('.');
  return i > 0 ? name.slice(0, i).toLowerCase() : name.toLowerCase();
}

/** Extract sandbox refs only; these are identifiers, never arbitrary fetch URLs. */
export function artifactReferences(content: string): string[] {
  return Array.from(content.matchAll(/\((\.?\/?artifacts?\/[^)\s]+)\)/gi), match => match[1]);
}

function refParts(src: string): { group: string; file: string } | null {
  const path = src.replace(/^\.?\/?/, '').split(/[?#]/)[0];
  const match = path.match(/^artifacts?\/(.+)$/i);
  if (!match) return null;
  try {
    const segments = match[1].split('/').filter(Boolean).map(decodeURIComponent);
    if (segments.some(segment =>
      segment === '..' || /[\\/]/.test(segment)
      || Array.from(segment).some(char => char.charCodeAt(0) < 32)
    )) return null;
    return { group: segments[0] || '', file: segments[segments.length - 1] || '' };
  } catch { return null; }
}

/** Exact files outrank directory aliases. Multi-file groups preserve the filename
 * on /open so a provider returning only ONE gallery member cannot substitute it
 * for every image. Historical snapshots may name that gallery after its first file.
 */
export function resolveArtifactRef(
  src: string,
  artifacts: ArtifactRefName[] | undefined,
  resolveUrl: (artifactId: string) => string,
  references: string[] = [],
): string | null {
  const items = artifacts || [];
  const base = (a: ArtifactRefName) => (a.name || '').split(/[\\/]/).pop() || '';
  const ref = refParts(src);
  if (ref) {
    const peers = references.map(refParts).filter(p => p?.group === ref.group);
    const filenames = new Set(peers.map(p => p!.file));
    const multiple = filenames.size > 1;
    let hit = items.find(a => base(a) === ref.file)
      || items.find(a => base(a) && stripExt(base(a)) === stripExt(ref.file))
      || items.find(a => a.name === ref.group || base(a) === ref.group)
      || items.find(a => base(a) && stripExt(base(a)) === stripExt(ref.group));
    if (!hit && multiple) {
      const groupItems = items.filter(a => filenames.has(base(a)));
      if (groupItems.length === 1) hit = groupItems[0];
    }
    if (!hit) return null;
    const target = resolveUrl(hit.artifactId);
    const directory = base(hit) === ref.group && !ref.group.includes('.') && ref.file !== ref.group;
    return (multiple || directory) && /\.[a-z0-9]+$/i.test(ref.file)
      ? `${target}${target.includes('?') ? '&' : '?'}filename=${encodeURIComponent(ref.file)}`
      : target;
  }
  if (/^\.?\/?artifacts?\//i.test(src)) return null;
  const tail = src.split(/[?#]/)[0].split('/').pop() || '';
  const hit = items.find(a => base(a) && (base(a) === tail || stripExt(base(a)) === stripExt(tail)));
  return hit ? resolveUrl(hit.artifactId) : null;
}
