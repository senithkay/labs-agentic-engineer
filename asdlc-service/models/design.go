package models

// DesignComponent describes a single component within a design.
// This matches the structured output schema from the AI Agent SDK.
type DesignComponent struct {
	Name                       string   `json:"name"`
	ComponentType              string   `json:"componentType"`
	Language                   string   `json:"language"`
	DependsOn                  []string `json:"dependsOn"`
	DbEngine                   string   `json:"dbEngine,omitempty"`
	Entrypoint                 string   `json:"entrypoint,omitempty"`
	Buildpack                  string   `json:"buildpack,omitempty"`
	AppPath                    string   `json:"appPath,omitempty"`
	OpenAPISpec                string   `json:"openAPISpec,omitempty"`
	ComponentAgentInstructions string   `json:"componentAgentInstructions,omitempty"`
}

// DesignComponents is a slice of DesignComponent.
type DesignComponents []DesignComponent

type Design struct {
	ProjectID         string            `json:"projectId"`
	OrgID             string            `json:"-"`
	Overview          string            `json:"overview"`
	Components        DesignComponents  `json:"components"`
	Status            string            `json:"status"`
	Version           int               `json:"version"`
	Versions          []ArtifactVersion `json:"versions,omitempty"`
	HasUnsavedChanges bool              `json:"hasUnsavedChanges"`
	SourceSpec        string            `json:"sourceSpec,omitempty"`
}
