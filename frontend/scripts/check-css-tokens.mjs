import { readFile, readdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const sourceRoot = path.join(frontendRoot, 'src');
const tokenFile = path.join(sourceRoot, 'styles', 'variables.css');
const supported = new Set(['.css', '.ts', '.tsx']);
const definitionPattern = /(--[\w-]+)\s*:/g;
const referencePattern = /var\(\s*(--[\w-]+)/g;
const stripComments = (content) => content.replace(/\/\*[\s\S]*?\*\//g, '').replace(/(^|\s)\/\/.*$/gm, '$1');
async function collectFiles(directory) { const entries = await readdir(directory, { withFileTypes: true }); const nested = await Promise.all(entries.map(async (entry) => { const absolute = path.join(directory, entry.name); if (entry.isDirectory()) return collectFiles(absolute); return supported.has(path.extname(entry.name)) ? [absolute] : []; })); return nested.flat(); }
const files = await collectFiles(sourceRoot);
const globalDefinitions = new Set([...stripComments(await readFile(tokenFile, 'utf8')).matchAll(definitionPattern)].map((match) => match[1]));
const undefinedReferences = new Map(); let referenceCount = 0;
for (const file of files) { const content = stripComments(await readFile(file, 'utf8')); const localDefinitions = new Set([...content.matchAll(definitionPattern)].map((match) => match[1])); for (const match of content.matchAll(referencePattern)) { referenceCount += 1; const token = match[1]; if (globalDefinitions.has(token) || localDefinitions.has(token)) continue; const locations = undefinedReferences.get(token) ?? new Set(); locations.add(path.relative(frontendRoot, file).replaceAll('\\', '/')); undefinedReferences.set(token, locations); } }
const undefinedTokens = [...undefinedReferences].sort(([left], [right]) => left.localeCompare(right));
if (undefinedTokens.length) { console.error('Undefined CSS variables:'); for (const [token, locations] of undefinedTokens) { console.error(`\n${token}`); for (const location of locations) console.error(`  ${location}`); } process.exitCode = 1; } else { console.log(`CSS token check passed (${referenceCount} references / ${globalDefinitions.size} global tokens).`); }
