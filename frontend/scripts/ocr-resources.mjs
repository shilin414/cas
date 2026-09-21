/** Pinned real official model archives, NOT 1.5 MB assumptions. */
import definition from '../src/features/ai-models/ocr/builtin-model.json' with { type: 'json' };

export const version = definition.version;
export const upstreamCommit = definition.upstream_commit;
export const resources = definition.resources.map((resource) => ({ ...resource }));
