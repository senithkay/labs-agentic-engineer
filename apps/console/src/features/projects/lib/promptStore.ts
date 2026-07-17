/**
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// The create-project prompt, persisted client-side (#150). The BE receives
// `prompt` on create but never hands it back (the Project read schema has no
// prompt field), so the console keeps its own copy to seed the first
// requirements-generation turn from the Spec card's "Generate spec" CTA.
// Keyed by (org, project); org matches what AppLayout passes the chat panel
// (`orgHandle ?? "default"`), so the writer (ProjectCreate) and the reader
// (AgentChatPanel) resolve the same key.

function key(org: string, project: string): string {
  return `aep.createPrompt.${org}.${project}`;
}

function safeLocalStorage(): Storage | null {
  try {
    return window.localStorage;
  } catch {
    return null; // SSR / privacy-mode denial — degrade to "no stored prompt"
  }
}

export function saveCreatePrompt(
  org: string,
  project: string,
  prompt: string,
): void {
  const text = prompt.trim();
  if (!text) return;
  safeLocalStorage()?.setItem(key(org, project), text);
}

export function readCreatePrompt(org: string, project: string): string | null {
  return safeLocalStorage()?.getItem(key(org, project)) ?? null;
}

export function clearCreatePrompt(org: string, project: string): void {
  safeLocalStorage()?.removeItem(key(org, project));
}

/**
 * The instruction the "Generate spec" CTA sends into the room turn (#150): an
 * explicit generate command (not the raw idea, which the agent might treat as
 * a chat opener) wrapping the stored create prompt. Falls back to a generic
 * instruction when no prompt was stored (older project / other browser /
 * cleared storage) — the CTA still works and the agent can ask for detail.
 */
export function buildSpecGenerationInstruction(prompt: string | null): string {
  const base =
    "Generate a complete requirements specification (requirements/requirements.md) for this project";
  return prompt && prompt.trim()
    ? `${base} based on the following idea:\n\n${prompt.trim()}`
    : `${base}.`;
}

/**
 * The instruction the "Generate / Re-generate design" CTA sends into the room
 * turn (#159): derive the component design from the current requirements. No
 * user prompt — the agent designs FROM the requirements already in the repo;
 * the agent's system prompt carries the design-file structure and schema.
 *
 * The CTA also mints the acceptance oracle in the same turn: the new console
 * only ever runs `requirements-chat` turns (never `design-generate`), so the
 * server-side design-generate steering that would author
 * `validation-criteria.json` never fires. Asking for it here scopes the oracle
 * to exactly the Generate-design action rather than every chat turn. See
 * docs/design/validation.md ("The acceptance oracle").
 */
export function buildDesignGenerationInstruction(): string {
  return (
    "Generate the complete component design for this project based on the " +
    "current requirements. If a design already exists, regenerate it to match " +
    "the current requirements. Then, as the final step, generate the validation " +
    "criteria."
  );
}
