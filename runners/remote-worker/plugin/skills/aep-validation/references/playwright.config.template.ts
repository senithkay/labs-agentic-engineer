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

// Template for tests/e2e/playwright.config.ts in the project repo.
// Copied by the aep-validation skill; adjust nothing except where a
// comment says so. Retries stay 0 — flake repair is the healer's job,
// and retries would mask the brittleness signal it needs.

import { defineConfig } from "@playwright/test";
import { primaryTarget } from "./lib/targets";

export default defineConfig({
  testDir: "./specs",
  // Serial: specs share one deployed environment; parallel runs would
  // race on server-side state and make failures non-deterministic.
  fullyParallel: false,
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 30_000,
  expect: { timeout: 10_000 },
  reporter: [
    ["list"],
    ["json", { outputFile: "test-results/results.json" }],
  ],
  outputDir: "test-results/artifacts",
  use: {
    baseURL: primaryTarget(),
    trace: "retain-on-failure",
  },
});
