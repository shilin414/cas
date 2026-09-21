import type { BrowserManifest } from '@/services/aiModels';
import definition from './builtin-model.json';

export const BUILTIN_OCR_NAME = definition.display_name;
export const BUILTIN_OCR_MODEL_ID = definition.model_id;
export const BUILTIN_OCR_VERSION = definition.version;

/** Portable manifest stored in the database; runtime validation expands URLs under Vite BASE_URL. */
export function builtinOcrManifest(): BrowserManifest {
  return {
    adapter: 'paddleocr_tiny',
    version: definition.version,
    resources: definition.resources.map((resource) => ({
      name: resource.name,
      url: `ocr-assets/${definition.version}/${resource.name}.tar`,
      sha256: resource.sha256,
      size_bytes: resource.size_bytes,
    })),
  };
}
