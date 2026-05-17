import { z } from "zod";

// SlimComponent — shape metadata only, no openAPISpec. The architect emits
// these via add_component / set_* tools so the UI can render component cards
// before the (large) OpenAPI YAML has streamed.
export const SlimComponent = z.object({
  name: z
    .string()
    .describe("Lowercase kebab-case component name, e.g. 'user-api'"),
  componentType: z
    .enum(["service", "web-app", "database"])
    .describe(
      "Component type: 'web-app' for frontends, 'service' for backend APIs, 'scheduled-task' for cron/batch jobs, 'database' for managed database instances",
    ),
  language: z
    .string()
    .describe(
      "Primary programming language and framework, e.g. 'Go', 'TypeScript / React', 'Ballerina'",
    ),
  dependsOn: z
    .array(z.string())
    .describe(
      "Names of other components this one depends on (must match other components' 'name' values exactly)",
    ),
  dbEngine: z
    .enum(["mysql", "mongodb"])
    .optional()
    .describe(
      "Database engine — required for componentType 'database'. Use 'mysql' for relational/transactional workloads, 'mongodb' for document/flexible-schema workloads.",
    ),
  entrypoint: z
    .enum(["deployment/service", "deployment/web-application"]).optional()
    .describe(
      "OpenChoreo component type: 'deployment/service' for backend APIs, 'deployment/web-application' for frontends/SPAs, Omit for 'database' components.",
    ),
  buildpack: z.literal("docker").optional().describe("Build strategy. Omit for 'database' components."),
  appPath: z
    .string()
    .optional()
    .describe(
      "Folder (directory) within the monorepo where this component's source code lives, relative to the repo root. This is NOT an HTTP route or API path — it is a filesystem path. Must NOT start with a leading slash. Examples: 'user-api', 'services/auth'. The coding agent will create files like '<appPath>/main.go', '<appPath>/Dockerfile', '<appPath>/workload.yaml'. Omit for 'database' components.",
    ),
  componentAgentInstructions: z
    .string()
    .optional()
    .describe(
      "Detailed implementation instructions for the Generator agent. Omit for 'database' components.",
    ),
});

export type SlimComponent = z.infer<typeof SlimComponent>;

// DesignComponent — slim + openAPISpec. This is the wire shape the BFF and
// console expect at data-finish; produced by DesignDoc.materialize().
export const DesignComponent = SlimComponent.extend({
  openAPISpec: z
    .string()
    .optional()
    .describe("Complete OpenAPI 3.0 YAML spec for this component. Empty string or omitted for 'database' components."),
});

export type DesignComponent = z.infer<typeof DesignComponent>;

export const ArchitectOutput = z.object({
  overview: z
    .string()
    .describe(
      "A 2-3 sentence architecture overview summarizing the system design, component structure, and communication patterns",
    ),
  components: z
    .array(DesignComponent)
    .describe("Deployable service components"),
});

export type ArchitectOutput = z.infer<typeof ArchitectOutput>;

export const ArchitectInput = z.object({
  projectName: z.string(),
  spec: z.string().describe("Specification document to design against"),
  previousDesign: ArchitectOutput.optional().describe(
    "Existing design to evolve — preserve component names and structure where possible",
  ),
  // Wireframes / domain-models live alongside the spec under
  // `.asdlc/requirements/`. The BFF passes the raw DSL keyed by canvas
  // name (without extension); the architect calls `read_wireframe(name)`
  // on demand to pull in the DSL when a screen flow is relevant.
  wireframes: z
    .record(z.string(), z.string())
    .optional()
    .describe(
      "Map of canvas name (e.g. 'wireframes', 'domain-model') to DSL text",
    ),
  availableWireframes: z
    .array(z.string())
    .optional()
    .describe(
      "List of canvas names available via the read_wireframe tool. Mentioned in the system prompt so the model knows what to fetch.",
    ),
});

export type ArchitectInput = z.infer<typeof ArchitectInput>;
